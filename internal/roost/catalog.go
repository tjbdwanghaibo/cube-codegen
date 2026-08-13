package roost

import (
	"fmt"
	"sort"
)

type modSpec struct {
	ImportPath  string
	Alias       string
	Constructor string
	Depends     []string
	Config      string
	DevService  string
}

var modCatalog = map[string]modSpec{
	"lock": {
		ImportPath: "github.com/tjbdwanghaibo/cube-kit/lock", Alias: "kitlock", Constructor: "kitlock.NewLockMod()",
	},
	"ops": {
		ImportPath: "github.com/tjbdwanghaibo/cube-kit/ops", Alias: "kitops", Constructor: "kitops.NewOpsMod()",
		Config: "ops:\n  enabled: true\n  addr: 127.0.0.1:9100\n  admin_enabled: false\n  admin_token: \"\"\n  allow_dev_token: false\n",
	},
	"statslog": {
		ImportPath: "github.com/tjbdwanghaibo/cube-kit/statslog", Alias: "kitstatslog", Constructor: "kitstatslog.NewStatsLogMod()",
		Config: "stats_log:\n  enabled: true\n  dir: log\n  interval: 1m\n",
	},
	"configdata": {
		ImportPath: "github.com/tjbdwanghaibo/cube-kit/configdata", Alias: "kitconfigdata", Constructor: "kitconfigdata.NewConfigDataMod()",
		Config: "config_data:\n  dir: configs/data\n",
	},
	"etcd": {
		ImportPath: "github.com/tjbdwanghaibo/cube-kit/etcd", Alias: "kitetcd", Constructor: "kitetcd.NewEtcdMod()",
		Config:     "etcd:\n  endpoints: 127.0.0.1:2379\n  username: \"\"\n  password: \"\"\n  service_prefix: /roost/services\n  lease_ttl: 10\n  advertise_addr: 127.0.0.1:9000\n",
		DevService: "etcd",
	},
	"redis": {
		ImportPath: "github.com/tjbdwanghaibo/cube-kit/redis", Alias: "kitredis", Constructor: "kitredis.NewRedisMod()",
		Config:     "redis:\n  addr: 127.0.0.1:6379\n  password: \"\"\n  db: 0\n  pool_size: 32\n  min_idle_conns: 4\n",
		DevService: "redis",
	},
	"mongo": {
		ImportPath: "github.com/tjbdwanghaibo/cube-kit/mongo", Alias: "kitmongo", Constructor: "kitmongo.NewMongoMod()",
		Config:     "mongo:\n  uri: mongodb://127.0.0.1:27017\n  connect_timeout: 5s\n  max_pool_size: 100\n  min_pool_size: 5\n",
		DevService: "mongo",
	},
	"nats": {
		ImportPath: "github.com/tjbdwanghaibo/cube-kit/nats", Alias: "kitnats", Constructor: "kitnats.NewNatsMod(nil)",
		Config:     "nats:\n  url: nats://127.0.0.1:4222\n  prefix: roost\n  worker_num: 8\n  reliable:\n    enabled: false\n",
		DevService: "nats",
	},
	"sync": {
		ImportPath: "github.com/tjbdwanghaibo/cube-kit/sync", Alias: "kitsync", Constructor: "kitsync.NewSyncMod(0)", Depends: []string{"nats"},
		Config: "sync:\n  transport: jetstream\n  prefix: roost.sync\n  storage: file\n  replicas: 1\n  publish_timeout: 3s\n",
	},
	"remote_entity": {
		ImportPath: "github.com/tjbdwanghaibo/cube-kit/remote_entity", Alias: "kitremoteentity", Constructor: "kitremoteentity.NewRemoteEntityMod(0)", Depends: []string{"redis"},
		Config: "remote_entity:\n  lock_ttl: 15s\n  retry_count: 3\n  retry_delay: 100ms\n  op_timeout: 3s\n  sync_retry_queue_cap: 1024\n  sync_retry_interval: 500ms\n  sync_retry_max_attempts: 10\n",
	},
}

var knownFeatures = map[string]bool{
	"protocol": true, "config": true, "entity": true, "nest": true,
	"event": true, "dao": true, "attribute": true, "webroute": true,
	"errcode":          true,
	"replication-quic": true, "replication-kcp": true, "replication-udp": true,
}

func resolveMods(requested []string) ([]string, error) {
	seen := map[string]bool{}
	visiting := map[string]bool{}
	var out []string
	var visit func(string) error
	visit = func(name string) error {
		spec, ok := modCatalog[name]
		if !ok {
			return fmt.Errorf("unknown kit mod %q", name)
		}
		if seen[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("kit mod dependency cycle at %q", name)
		}
		visiting[name] = true
		for _, dependency := range spec.Depends {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visiting[name] = false
		seen[name] = true
		out = append(out, name)
		return nil
	}
	for _, name := range requested {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func allProjectMods(m Manifest) []string {
	seen := map[string]bool{}
	for _, mod := range m.SharedMods {
		seen[mod] = true
	}
	for _, svc := range m.Services {
		for _, mod := range svc.Mods {
			seen[mod] = true
		}
	}
	out := make([]string, 0, len(seen))
	for mod := range seen {
		out = append(out, mod)
	}
	sort.Strings(out)
	return out
}
