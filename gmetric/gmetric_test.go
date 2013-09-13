package gmetric_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"testing"
	"text/template"
	"time"

	"github.com/daaku/go.freeport"
	"github.com/daaku/go.subset"

	"github.com/daaku/go.ganglia/gmetric"
	"github.com/daaku/go.ganglia/gmon"
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
  host_dmax = 864000
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

var errFixed = errors.New("fixed error")

type errWriter int

func (e errWriter) Write(b []byte) (int, error) {
	return 0, errFixed
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

	if err := h.Client.Open(); err != nil {
		h.T.Fatal(err)
	}
}

func (h *harness) Stop() {
	if err := h.Client.Close(); err != nil {
		h.T.Fatal(err)
	}

	if err := h.Cmd.Process.Kill(); err != nil {
		h.T.Fatal(err)
	}

	if err := os.Remove(h.ConfigPath); err != nil {
		h.T.Fatal(err)
	}
}

func (h *harness) State() *gmon.Ganglia {
	addr := fmt.Sprintf("%s:%d", localhostIP, h.Port)
	ganglia, err := gmon.RemoteRead("tcp", addr)
	if err != nil {
		h.T.Fatal(err)
	}
	return ganglia
}

func (h *harness) ContainsMetric(m *gmon.Metric) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		g := h.State()
		for _, cluster := range g.Clusters {
			for _, host := range cluster.Hosts {
				for _, metric := range host.Metrics {
					if subset.Check(m, &metric) {
						return
					}
				}
			}
		}

		if time.Now().After(deadline) {
			h.T.Fatalf("did not find metric %+v in\n%+v", m, g)
		}
	}
}

func newHarness(t *testing.T) *harness {
	h := &harness{T: t}
	h.Start()
	return h
}

func TestUint32Metric(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	defer h.Stop()

	m := &gmetric.Metric{
		Name:         "uint32_metric",
		Host:         "localhost",
		ValueType:    gmetric.ValueUint32,
		Units:        "count",
		Slope:        gmetric.SlopeBoth,
		TickInterval: 20 * time.Second,
		Lifetime:     24 * time.Hour,
	}
	const val = 10

	if err := h.Client.SendMeta(m); err != nil {
		t.Fatal(err)
	}

	if err := h.Client.SendValue(m, val); err != nil {
		t.Fatal(err)
	}

	h.ContainsMetric(&gmon.Metric{
		Name:  m.Name,
		Value: fmt.Sprint(val),
		Unit:  m.Units,
		Tn:    1,
		Tmax:  20,
		Slope: "both",
	})
}

func TestStringMetric(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	defer h.Stop()

	m := &gmetric.Metric{
		Name:         "string_metric",
		Host:         "localhost",
		ValueType:    gmetric.ValueString,
		Units:        "count",
		Slope:        gmetric.SlopeBoth,
		TickInterval: 20 * time.Second,
		Lifetime:     24 * time.Hour,
	}
	const val = "hello"

	if err := h.Client.SendMeta(m); err != nil {
		t.Fatal(err)
	}

	if err := h.Client.SendValue(m, val); err != nil {
		t.Fatal(err)
	}

	h.ContainsMetric(&gmon.Metric{
		Name:  m.Name,
		Unit:  m.Units,
		Value: val,
		Tn:    1,
		Tmax:  20,
		Slope: "both",
	})
}

func TestFloatMetric(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	defer h.Stop()

	m := &gmetric.Metric{
		Name:         "float_metric",
		Host:         "localhost",
		ValueType:    gmetric.ValueFloat32,
		Units:        "count",
		Slope:        gmetric.SlopeBoth,
		TickInterval: 20 * time.Second,
		Lifetime:     24 * time.Hour,
	}

	if err := h.Client.SendMeta(m); err != nil {
		t.Fatal(err)
	}

	if err := h.Client.SendValue(m, 3.14); err != nil {
		t.Fatal(err)
	}

	h.ContainsMetric(&gmon.Metric{
		Name:  m.Name,
		Unit:  m.Units,
		Value: "3.140000",
		Tn:    1,
		Tmax:  20,
		Slope: "both",
	})
}

func TestExtras(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	defer h.Stop()

	m := &gmetric.Metric{
		Name:         "extras_metric",
		Spoof:        "127.0.0.1:localhost_spoof",
		Title:        "the simple title",
		Description:  "the simple description",
		Host:         "localhost",
		Group:        "simple_group",
		ValueType:    gmetric.ValueString,
		Units:        "count",
		Slope:        gmetric.SlopeBoth,
		TickInterval: 20 * time.Second,
		Lifetime:     24 * time.Hour,
	}
	const val = "hello"

	if err := h.Client.SendMeta(m); err != nil {
		t.Fatal(err)
	}

	if err := h.Client.SendValue(m, val); err != nil {
		t.Fatal(err)
	}

	h.ContainsMetric(&gmon.Metric{
		Name:  m.Name,
		Unit:  m.Units,
		Value: val,
		Tn:    1,
		Tmax:  20,
		Slope: "both",
		ExtraData: gmon.ExtraData{
			ExtraElements: []gmon.ExtraElement{
				gmon.ExtraElement{Name: "GROUP", Val: m.Group},
				gmon.ExtraElement{Name: "SPOOF_HOST", Val: m.Spoof},
				gmon.ExtraElement{Name: "DESC", Val: m.Description},
				gmon.ExtraElement{Name: "TITLE", Val: m.Title},
			},
		},
	})
}

func TestNoAddrs(t *testing.T) {
	t.Parallel()
	c := &gmetric.Client{}
	if c.Open() == nil {
		t.Fatal("was expecting an error")
	}
	if c.Close() == nil {
		t.Fatal("was expecting an error")
	}
}

func TestEncodeMetaWriterError(t *testing.T) {
	t.Parallel()
	m := &gmetric.Metric{
		Name:         "encode_meta_panic_metric",
		Host:         "localhost",
		ValueType:    gmetric.ValueUint32,
		Units:        "count",
		Slope:        gmetric.SlopeBoth,
		TickInterval: 20 * time.Second,
		Lifetime:     24 * time.Hour,
	}
	if err := m.EncodeMeta(errWriter(0)); err != errFixed {
		t.Fatalf("was expecting errFixed but got %s", err)
	}
}

func TestEncodeValueWriterError(t *testing.T) {
	t.Parallel()
	m := &gmetric.Metric{
		Name:         "string_metric",
		Host:         "localhost",
		ValueType:    gmetric.ValueString,
		Units:        "count",
		Slope:        gmetric.SlopeBoth,
		TickInterval: 20 * time.Second,
		Lifetime:     24 * time.Hour,
	}
	if err := m.EncodeValue(errWriter(0), "val"); err != errFixed {
		t.Fatalf("was expecting errFixed but got %s", err)
	}
}
