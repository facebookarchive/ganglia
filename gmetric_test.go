package gmetric_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"testing"
	"text/template"
	"time"

	"github.com/daaku/go.freeport"
	"github.com/daaku/go.gmetric"
)

const localhostIP = "127.0.0.1"

var configTemplate, configTemplateErr = template.New("config").Parse(`
globals {
  daemonize = no
  setuid = false
  debug_level = 0
  max_udp_msg_len = 1472
  mute = no
  deaf = no
  allow_extra_data = yes
}

cluster {
  name = "gmetric_test"
  owner = "gmetric_test"
  latlong = "gmetric_test"
  url = "gmetric_test"
}

host {
  location = "gmetric_test"
}

udp_recv_channel {
  port = {{.Port}}
  family = inet4
}

udp_recv_channel {
  port = {{.Port}}
  family = inet6
}

tcp_accept_channel {
  port = {{.Port}}
}
`)

func init() {
	if configTemplateErr != nil {
		panic(configTemplateErr)
	}
}

type harness struct {
	Client     *gmetric.Client
	Port       int
	T          *testing.T
	ConfigPath string
	Cmd        *exec.Cmd
}

func (h *harness) Start() {
	var err error
	if h.Port == 0 {
		if h.Port, err = freeport.Get(); err != nil {
			h.T.Fatal(err)
		}
	}

	cf, err := ioutil.TempFile("", "gmetric_test_gmond_conf")
	if err != nil {
		h.T.Fatal(err)
	}
	h.ConfigPath = cf.Name()

	if err := configTemplate.Execute(cf, h); err != nil {
		h.T.Fatal(err)
	}

	if err := cf.Close(); err != nil {
		h.T.Fatal(err)
	}

	h.Cmd = exec.Command("gmond", "--conf", h.ConfigPath)
	h.Cmd.Stderr = os.Stderr
	h.Cmd.Stdout = os.Stdout
	if err := h.Cmd.Start(); err != nil {
		h.T.Fatal(err)
	}

	// Wait until TCP socket is active to ensure we don't progress until the
	// server is ready to accept.
	for {
		if c, err := net.Dial("tcp", fmt.Sprintf("%s:%d", localhostIP, h.Port)); err == nil {
			c.Close()
			break
		}
	}

	h.Client = &gmetric.Client{
		Addr: []*net.UDPAddr{
			&net.UDPAddr{IP: net.ParseIP(localhostIP), Port: h.Port},
		},
	}

	if err := h.Client.Start(); err != nil {
		h.T.Fatal(err)
	}
}

func (h *harness) Stop() {
	if err := h.Client.Stop(); err != nil {
		h.T.Fatal(err)
	}

	if err := h.Cmd.Process.Kill(); err != nil {
		h.T.Fatal(err)
	}

	if err := os.Remove(h.ConfigPath); err != nil {
		h.T.Fatal(err)
	}
}

func (h *harness) Xml() []byte {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", localhostIP, h.Port))
	if err != nil {
		h.T.Fatal(err)
	}
	defer conn.Close()

	b, err := ioutil.ReadAll(conn)
	if err != nil {
		h.T.Fatal(err)
	}

	return b
}

func (h *harness) XmlContains(s []byte) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		xml := h.Xml()
		if bytes.Contains(xml, s) {
			return
		}
		if time.Now().After(deadline) {
			h.T.Fatalf("did not find %s in xml\n%s", s, xml)
		}
	}
}

func newHarness(t *testing.T) *harness {
	h := &harness{T: t}
	h.Start()
	return h
}

func TestSimpleMetric(t *testing.T) {
	h := newHarness(t)
	defer h.Stop()

	m := &gmetric.Metric{
		Name:         "simple_metric",
		Host:         "localhost",
		ValueType:    gmetric.ValueUint32,
		Units:        "count",
		Slope:        gmetric.SlopeBoth,
		TickInterval: 20 * time.Second,
		Lifetime:     24 * time.Hour,
	}

	if err := h.Client.SendMeta(m); err != nil {
		t.Fatal(err)
	}

	if err := h.Client.SendValue(m, 10); err != nil {
		t.Fatal(err)
	}

	h.XmlContains([]byte(`<METRIC NAME="simple_metric" VAL="10" TYPE="uint32" UNITS="count" TN="1" TMAX="20" DMAX="86400" SLOPE="both">`))
}
