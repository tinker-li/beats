// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package key

import (
	"time"

	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/metricbeat/mb"
	"github.com/elastic/beats/metricbeat/mb/parse"
	"github.com/elastic/beats/metricbeat/module/redis"

	rd "github.com/garyburd/redigo/redis"
)

var (
	debugf = logp.MakeDebug("redis-key")
)

func init() {
	mb.Registry.MustAddMetricSet("redis", "key", New,
		mb.WithHostParser(parse.PassThruHostParser),
	)
}

// MetricSet for fetching Redis server information and statistics.
type MetricSet struct {
	mb.BaseMetricSet
	pool     *rd.Pool
	patterns []KeyPattern
}

// KeyPattern contains the information required to query keys
type KeyPattern struct {
	Keyspace uint   `config:"keyspace"`
	Pattern  string `config:"pattern" validate:"required"`
	Limit    uint   `config:"limit"`
}

// New creates new instance of MetricSet
func New(base mb.BaseMetricSet) (mb.MetricSet, error) {
	// Unpack additional configuration options.
	config := struct {
		IdleTimeout time.Duration `config:"idle_timeout"`
		Network     string        `config:"network"`
		MaxConn     int           `config:"maxconn" validate:"min=1"`
		Password    string        `config:"password"`
		Patterns    []KeyPattern  `config:"key.patterns" validate:"nonzero,required"`
	}{
		Network:  "tcp",
		MaxConn:  10,
		Password: "",
	}
	err := base.Module().UnpackConfig(&config)
	if err != nil {
		return nil, err
	}

	return &MetricSet{
		BaseMetricSet: base,
		pool: redis.CreatePool(base.Host(), config.Password, config.Network,
			config.MaxConn, config.IdleTimeout, base.Module().Config().Timeout),
		patterns: config.Patterns,
	}, nil
}

// Fetch fetches information from Redis keys
func (m *MetricSet) Fetch(r mb.ReporterV2) {
	conn := m.pool.Get()
	for _, p := range m.patterns {
		if err := redis.Select(conn, p.Keyspace); err != nil {
			logp.Err("Failed to select keyspace %d: %s", p.Keyspace, err)
			continue
		}

		keys, err := redis.FetchKeys(conn, p.Pattern, p.Limit)
		if err != nil {
			logp.Err("Failed to fetch list of keys in keyspace %d with pattern '%s': %s", p.Keyspace, p.Pattern, err)
			continue
		}
		if p.Limit > 0 && len(keys) > int(p.Limit) {
			debugf("Collecting stats for %d keys, but there are more available for pattern '%s' in keyspace %d", p.Limit)
			keys = keys[:p.Limit]
		}

		for _, key := range keys {
			keyInfo, err := redis.FetchKeyInfo(conn, key)
			if err != nil {
				logp.Err("Failed to fetch key info for key %s in keyspace %d", key, p.Keyspace)
				continue
			}
			eventMapping(r, p.Keyspace, keyInfo)
		}
	}
}

// Close connections
func (m *MetricSet) Close() error {
	return m.pool.Close()
}