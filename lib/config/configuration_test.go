/*
Copyright 2015 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"bytes"
	"encoding/base64"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/service"
	"github.com/gravitational/teleport/lib/services"

	"golang.org/x/crypto/ssh"
	"gopkg.in/check.v1"
)

// bootstrap check
func TestConfig(t *testing.T) { check.TestingT(t) }

// register test suite
type ConfigTestSuite struct {
	tempDir              string
	configFile           string // good
	configFileNoContent  string // empty file
	configFileBadContent string // garbage inside
	configFileStatic     string // file from a static YAML fixture
}

var _ = check.Suite(&ConfigTestSuite{})

func (s *ConfigTestSuite) SetUpSuite(c *check.C) {
	var err error
	s.tempDir, err = ioutil.TempDir("", "teleport-config")
	if err != nil {
		c.FailNow()
	}
	// create a good config file fixture
	s.configFile = filepath.Join(s.tempDir, "good-config.yaml")
	if err = ioutil.WriteFile(s.configFile, []byte(makeConfigFixture()), 0660); err != nil {
		c.FailNow()
	}
	// create a static config file fixture
	s.configFileStatic = filepath.Join(s.tempDir, "static-config.yaml")
	if err = ioutil.WriteFile(s.configFileStatic, []byte(StaticConfigString), 0660); err != nil {
		c.FailNow()
	}
	// create an empty config file
	s.configFileNoContent = filepath.Join(s.tempDir, "empty-config.yaml")
	if err = ioutil.WriteFile(s.configFileNoContent, []byte(""), 0660); err != nil {
		c.FailNow()
	}
	// create a bad config file fixture
	s.configFileBadContent = filepath.Join(s.tempDir, "bad-config.yaml")
	if err = ioutil.WriteFile(s.configFileBadContent, []byte("bad-data!"), 0660); err != nil {
		c.FailNow()
	}
}

func (s *ConfigTestSuite) TearDownSuite(c *check.C) {
	os.RemoveAll(s.tempDir)
}

func (s *ConfigTestSuite) TestSampleConfig(c *check.C) {
	// generate sample config and write it into a temp file:
	sfc := MakeSampleFileConfig()
	c.Assert(sfc, check.NotNil)
	fn := filepath.Join(c.MkDir(), "default-config.yaml")
	err := ioutil.WriteFile(fn, []byte(sfc.DebugDumpToYAML()), 0660)
	c.Assert(err, check.IsNil)

	// make sure it could be parsed:
	fc, err := ReadFromFile(fn)
	c.Assert(err, check.IsNil)

	// validate a couple of values:
	c.Assert(fc.Limits.MaxUsers, check.Equals, defaults.LimiterMaxConcurrentUsers)
	c.Assert(fc.Global.DataDir, check.Equals, defaults.DataDir)
	c.Assert(fc.Logger.Severity, check.Equals, "INFO")
}

func (s *ConfigTestSuite) TestConfigReading(c *check.C) {
	// invalid config file type:
	conf, err := ReadFromFile("/bin/true")
	c.Assert(conf, check.IsNil)
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, ".*invalid configuration file type.*")
	// non-existing file:
	conf, err = ReadFromFile("/heaven/trees/apple.ymL")
	c.Assert(conf, check.IsNil)
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, ".*failed to open file.*")
	// bad content:
	conf, err = ReadFromFile(s.configFileBadContent)
	c.Assert(err, check.NotNil)
	// mpty config (must not fail)
	conf, err = ReadFromFile(s.configFileNoContent)
	c.Assert(err, check.IsNil)
	c.Assert(conf, check.NotNil)

	// static config
	conf, err = ReadFromFile(s.configFile)
	c.Assert(err, check.IsNil)
	c.Assert(conf, check.NotNil)
	c.Assert(conf.NodeName, check.Equals, NodeName)
	c.Assert(conf.AuthServers, check.DeepEquals, []string{"auth0.server.example.org:3024", "auth1.server.example.org:3024"})
	c.Assert(conf.Limits.MaxConnections, check.Equals, int64(100))
	c.Assert(conf.Limits.MaxUsers, check.Equals, 5)
	c.Assert(conf.Limits.Rates, check.DeepEquals, ConnectionRates)
	c.Assert(conf.Logger.Output, check.Equals, "stderr")
	c.Assert(conf.Logger.Severity, check.Equals, "INFO")
	c.Assert(conf.Storage.Type, check.Equals, "bolt")
	c.Assert(conf.DataDir, check.Equals, "/path/to/data")
	c.Assert(conf.Auth.Enabled(), check.Equals, true)
	c.Assert(conf.Auth.ListenAddress, check.Equals, "tcp://auth")
	c.Assert(conf.SSH.Configured(), check.Equals, true)
	c.Assert(conf.SSH.Enabled(), check.Equals, true)
	c.Assert(conf.SSH.ListenAddress, check.Equals, "tcp://ssh")
	c.Assert(conf.SSH.Labels, check.DeepEquals, Labels)
	c.Assert(conf.SSH.Commands, check.DeepEquals, CommandLabels)
	c.Assert(conf.Proxy.Configured(), check.Equals, true)
	c.Assert(conf.Proxy.Enabled(), check.Equals, true)
	c.Assert(conf.Proxy.KeyFile, check.Equals, "/etc/teleport/proxy.key")
	c.Assert(conf.Proxy.CertFile, check.Equals, "/etc/teleport/proxy.crt")
	c.Assert(conf.Proxy.ListenAddress, check.Equals, "tcp://proxy_ssh_addr")
	c.Assert(conf.Proxy.WebAddr, check.Equals, "tcp://web_addr")
	c.Assert(conf.Proxy.TunAddr, check.Equals, "reverse_tunnel_address:3311")

	// good config from file
	conf, err = ReadFromFile(s.configFileStatic)
	c.Assert(err, check.IsNil)
	c.Assert(conf, check.NotNil)
	checkStaticConfig(c, conf)

	// good config from base64 encoded string
	conf, err = ReadFromString(base64.StdEncoding.EncodeToString([]byte(StaticConfigString)))
	c.Assert(err, check.IsNil)
	c.Assert(conf, check.NotNil)
	checkStaticConfig(c, conf)
}

func (s *ConfigTestSuite) TestLabelParsing(c *check.C) {
	var conf service.SSHConfig
	var err error
	// empty spec. no errors, no labels
	err = parseLabels("", &conf)
	c.Assert(err, check.IsNil)
	c.Assert(conf.CmdLabels, check.IsNil)
	c.Assert(conf.Labels, check.IsNil)

	// simple static labels
	err = parseLabels(`key=value,more="much better"`, &conf)
	c.Assert(err, check.IsNil)
	c.Assert(conf.CmdLabels, check.NotNil)
	c.Assert(conf.CmdLabels, check.HasLen, 0)
	c.Assert(conf.Labels, check.DeepEquals, map[string]string{
		"key":  "value",
		"more": "much better",
	})

	// static labels + command labels
	err = parseLabels(`key=value,more="much better",arch=[5m2s:/bin/uname -m "p1 p2"]`, &conf)
	c.Assert(err, check.IsNil)
	c.Assert(conf.Labels, check.DeepEquals, map[string]string{
		"key":  "value",
		"more": "much better",
	})
	c.Assert(conf.CmdLabels, check.DeepEquals, services.CommandLabels{
		"arch": &services.CommandLabelV2{
			Period:  services.NewDuration(time.Minute*5 + time.Second*2),
			Command: []string{"/bin/uname", "-m", `"p1 p2"`},
		},
	})
}

func (s *ConfigTestSuite) TestTrustedClusters(c *check.C) {
	err := readTrustedClusters(nil, nil)
	c.Assert(err, check.IsNil)

	var conf service.Config
	err = readTrustedClusters([]TrustedCluster{
		{
			AllowedLogins: "vagrant, root",
			KeyFile:       "../../fixtures/trusted_clusters/cluster-a",
			TunnelAddr:    "one,two",
		},
	}, &conf)
	c.Assert(err, check.IsNil)
	authorities := conf.Auth.Authorities
	c.Assert(len(authorities), check.Equals, 2)
	c.Assert(authorities[0].GetClusterName(), check.Equals, "cluster-a")
	c.Assert(authorities[0].GetType(), check.Equals, services.HostCA)
	c.Assert(len(authorities[0].GetCheckingKeys()), check.Equals, 1)
	c.Assert(authorities[1].GetClusterName(), check.Equals, "cluster-a")
	c.Assert(authorities[1].GetType(), check.Equals, services.UserCA)
	c.Assert(len(authorities[1].GetCheckingKeys()), check.Equals, 1)
	_, _, _, _, err = ssh.ParseAuthorizedKey(authorities[1].GetCheckingKeys()[0])
	c.Assert(err, check.IsNil)

	tunnels := conf.ReverseTunnels
	c.Assert(len(tunnels), check.Equals, 1)
	c.Assert(tunnels[0].GetClusterName(), check.Equals, "cluster-a")
	c.Assert(len(tunnels[0].GetDialAddrs()), check.Equals, 2)
	c.Assert(tunnels[0].GetDialAddrs()[0], check.Equals, "tcp://one:3024")
	c.Assert(tunnels[0].GetDialAddrs()[1], check.Equals, "tcp://two:3024")

	// invalid data:
	err = readTrustedClusters([]TrustedCluster{
		{
			AllowedLogins: "vagrant, root",
			KeyFile:       "non-existing",
			TunnelAddr:    "one,two",
		},
	}, &conf)
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, "^.*reading trusted cluster keys.*$")
	err = readTrustedClusters([]TrustedCluster{
		{
			KeyFile:    "../../fixtures/trusted_clusters/cluster-a",
			TunnelAddr: "one,two",
		},
	}, &conf)
	c.Assert(err, check.ErrorMatches, ".*needs allow_logins parameter")
	conf.ReverseTunnels = nil
	err = readTrustedClusters([]TrustedCluster{
		{
			KeyFile:       "../../fixtures/trusted_clusters/cluster-a",
			AllowedLogins: "vagrant",
			TunnelAddr:    "",
		},
	}, &conf)
	c.Assert(err, check.IsNil)
	c.Assert(len(conf.ReverseTunnels), check.Equals, 0)
}

func (s *ConfigTestSuite) TestDynamicConfig(c *check.C) {
	falseBool := false

	tests := []struct {
		inConfigString   string
		outFileConfig    *bool // DynamicConfig set in config.FileConfig
		outDefaultConfig bool  // DynamicConfig set in default service.Config
		outConfig        bool  // DynamicConfig set in service.Config
	}{

		// 0 - nothing defined, final DynamicConfig should be false
		{
			`
teleport:
`,
			nil,
			false,
			false,
		},
		// 1 - define a backend, final DynamicConfig should be true
		{
			`
teleport:
  storage:
    type: bolt
`,
			nil,
			false,
			true,
		},
		// 2 - define a backend and override, final DynamicConfig should be false
		{
			`
teleport:
  storage:
    type: bolt

auth_service:
  dynamic_config: false
`,
			&falseBool,
			false,
			false,
		},
		// 3 - test legacy setting
		{
			`
teleport:
  seed_config: true
`,
			nil,
			false,
			true,
		},
	}

	// run tests
	for i, tt := range tests {
		comment := check.Commentf("Test %v", i)

		// read in the file configuration
		fc, err := ReadConfig(bytes.NewBufferString(tt.inConfigString))
		c.Assert(err, check.IsNil, comment)
		if fc.Auth.DynamicConfig != nil {
			c.Assert(*fc.Auth.DynamicConfig, check.Equals, *tt.outFileConfig, comment)
		} else {
			c.Assert(fc.Auth.DynamicConfig, check.Equals, tt.outFileConfig, comment)
		}

		// create a default configuration
		conf := service.MakeDefaultConfig()
		c.Assert(conf.Auth.DynamicConfig, check.Equals, tt.outDefaultConfig, comment)

		// apply the file configuration to the default configuration
		err = ApplyFileConfig(fc, conf)
		c.Assert(err, check.IsNil)
		c.Assert(conf.Auth.DynamicConfig, check.Equals, tt.outConfig, comment)
	}
}

func (s *ConfigTestSuite) TestApplyConfig(c *check.C) {
	conf, err := ReadConfig(bytes.NewBufferString(SmallConfigString))
	c.Assert(err, check.IsNil)
	c.Assert(conf, check.NotNil)

	cfg := service.MakeDefaultConfig()
	err = ApplyFileConfig(conf, cfg)
	c.Assert(err, check.IsNil)
	c.Assert(cfg.Auth.StaticTokens, check.DeepEquals, []services.ProvisionToken{
		{
			Token:   "xxx",
			Roles:   teleport.Roles([]teleport.Role{"Proxy", "Node"}),
			Expires: time.Unix(0, 0),
		},
		{
			Token:   "yyy",
			Roles:   teleport.Roles([]teleport.Role{"Auth"}),
			Expires: time.Unix(0, 0),
		},
	})
	c.Assert(cfg.Auth.DomainName, check.Equals, "magadan")
	c.Assert(cfg.AdvertiseIP, check.DeepEquals, net.ParseIP("10.10.10.1"))

	c.Assert(cfg.Proxy.Enabled, check.Equals, true)
	c.Assert(cfg.Proxy.WebAddr.FullAddress(), check.Equals, "tcp://webhost:3080")
	c.Assert(cfg.Proxy.ReverseTunnelListenAddr.FullAddress(), check.Equals, "tcp://tunnelhost:1001")
}

// TestLegacyU2FTransformation ensures that the legacy format for U2F gets transformed
// into the new format that we are using now for backward compatibility.
func (s *ConfigTestSuite) TestLegacyU2FTransformation(c *check.C) {
	conf, err := ReadConfig(bytes.NewBufferString(LegacyAuthenticationSection))
	c.Assert(err, check.IsNil)
	c.Assert(conf, check.NotNil)

	cfg := service.MakeDefaultConfig()
	err = ApplyFileConfig(conf, cfg)
	c.Assert(err, check.IsNil)

	c.Assert(cfg.Auth.U2F.GetAppID(), check.Equals, "https://graviton:3080")
	c.Assert(cfg.Auth.U2F.GetFacets(), check.DeepEquals, []string{"https://graviton:3080"})
}

func checkStaticConfig(c *check.C, conf *FileConfig) {
	c.Assert(conf.AuthToken, check.Equals, "xxxyyy")
	c.Assert(conf.SSH.Enabled(), check.Equals, false)      // YAML treats 'no' as False
	c.Assert(conf.Proxy.Configured(), check.Equals, false) // Missing "proxy_service" section must lead to 'not configured'
	c.Assert(conf.Proxy.Enabled(), check.Equals, true)     // Missing "proxy_service" section must lead to 'true'
	c.Assert(conf.Proxy.Disabled(), check.Equals, false)   // Missing "proxy_service" does NOT mean it's been disabled
	c.Assert(conf.AdvertiseIP.String(), check.Equals, "10.10.10.1")
	c.Assert(conf.PIDFile, check.Equals, "/var/run/teleport.pid")

	c.Assert(conf.Limits.MaxConnections, check.Equals, int64(90))
	c.Assert(conf.Limits.MaxUsers, check.Equals, 91)
	c.Assert(conf.Limits.Rates, check.HasLen, 2)
	c.Assert(conf.Limits.Rates[0].Average, check.Equals, int64(70))
	c.Assert(conf.Limits.Rates[0].Burst, check.Equals, int64(71))
	c.Assert(conf.Limits.Rates[0].Period.String(), check.Equals, "1m1s")
	c.Assert(conf.Limits.Rates[1].Average, check.Equals, int64(170))
	c.Assert(conf.Limits.Rates[1].Burst, check.Equals, int64(171))
	c.Assert(conf.Limits.Rates[1].Period.String(), check.Equals, "10m10s")

	c.Assert(conf.SSH.Disabled(), check.Equals, true) // "ssh_service" has been explicitly set to "no"
	c.Assert(conf.SSH.Commands, check.HasLen, 2)
	c.Assert(conf.SSH.Commands[0].Name, check.Equals, "hostname")
	c.Assert(conf.SSH.Commands[0].Command, check.DeepEquals, []string{"/bin/hostname"})
	c.Assert(conf.SSH.Commands[0].Period.Nanoseconds(), check.Equals, int64(10000000))
	c.Assert(conf.SSH.Commands[1].Name, check.Equals, "date")
	c.Assert(conf.SSH.Commands[1].Command, check.DeepEquals, []string{"/bin/date"})
	c.Assert(conf.SSH.Commands[1].Period.Nanoseconds(), check.Equals, int64(20000000))

	c.Assert(conf.Global.Keys[0].PrivateKey, check.Equals, "private key")
	c.Assert(conf.Global.Keys[0].Cert, check.Equals, "node.cert")
	c.Assert(conf.Global.Keys[1].PrivateKeyFile, check.Equals, "/proxy.key.file")
	c.Assert(conf.Global.Keys[1].CertFile, check.Equals, "/proxy.cert.file")
	c.Assert(conf.Auth.Authorities[0].Type, check.Equals, services.HostCA)
	c.Assert(conf.Auth.Authorities[0].DomainName, check.Equals, "example.com")
	c.Assert(conf.Auth.Authorities[0].CheckingKeys[0], check.Equals, "checking key 1")
	c.Assert(conf.Auth.Authorities[0].CheckingKeyFiles[0], check.Equals, "/ca.checking.key")
	c.Assert(conf.Auth.Authorities[0].SigningKeys[0], check.Equals, "signing key 1")
	c.Assert(conf.Auth.Authorities[0].SigningKeyFiles[0], check.Equals, "/ca.signing.key")
	c.Assert(conf.Auth.ReverseTunnels, check.DeepEquals, []ReverseTunnel{
		{
			DomainName: "tunnel.example.com",
			Addresses:  []string{"com-1", "com-2"},
		},
		{
			DomainName: "tunnel.example.org",
			Addresses:  []string{"org-1"},
		},
	})
	c.Assert(conf.Auth.StaticTokens, check.DeepEquals,
		[]StaticToken{"proxy,node:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "auth:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
}

var (
	NodeName        = "edsger.example.com"
	AuthServers     = []string{"auth0.server.example.org:3024", "auth1.server.example.org:3024"}
	ConnectionRates = []ConnectionRate{
		{
			Period:  time.Minute,
			Average: 5,
			Burst:   10,
		},
		{
			Period:  time.Minute * 10,
			Average: 10,
			Burst:   100,
		},
	}
	Labels = map[string]string{
		"name": "mongoserver",
		"role": "slave",
	}
	CommandLabels = []CommandLabel{
		{
			Name:    "os",
			Command: []string{"uname", "-o"},
			Period:  time.Minute * 15,
		},
		{
			Name:    "hostname",
			Command: []string{"/bin/hostname"},
			Period:  time.Millisecond * 10,
		},
	}
)

// makeConfigFixture returns a valid content for teleport.yaml file
func makeConfigFixture() string {
	conf := FileConfig{}

	// common config:
	conf.NodeName = NodeName
	conf.DataDir = "/path/to/data"
	conf.AuthServers = AuthServers
	conf.Limits.MaxConnections = 100
	conf.Limits.MaxUsers = 5
	conf.Limits.Rates = ConnectionRates
	conf.Logger.Output = "stderr"
	conf.Logger.Severity = "INFO"
	conf.Storage.Type = "bolt"

	// auth service:
	conf.Auth.EnabledFlag = "Yeah"
	conf.Auth.ListenAddress = "tcp://auth"

	// ssh service:
	conf.SSH.EnabledFlag = "true"
	conf.SSH.ListenAddress = "tcp://ssh"
	conf.SSH.Labels = Labels
	conf.SSH.Commands = CommandLabels

	// proxy-service:
	conf.Proxy.EnabledFlag = "yes"
	conf.Proxy.ListenAddress = "tcp://proxy"
	conf.Proxy.KeyFile = "/etc/teleport/proxy.key"
	conf.Proxy.CertFile = "/etc/teleport/proxy.crt"
	conf.Proxy.ListenAddress = "tcp://proxy_ssh_addr"
	conf.Proxy.WebAddr = "tcp://web_addr"
	conf.Proxy.TunAddr = "reverse_tunnel_address:3311"

	return conf.DebugDumpToYAML()
}
