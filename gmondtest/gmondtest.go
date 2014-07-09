// Package gmondtest provides test helpers for gmond.
package gmondtest

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"testing"
	"text/template"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookgo/freeport"
	"github.com/facebookgo/subset"
	"github.com/facebookgo/waitout"

	"github.com/facebookgo/ganglia/gmetric"
	"github.com/facebookgo/ganglia/gmon"
)

var gmondServerStarted = []byte("Starting TCP listener thread")

const localhostIP = "127.0.0.1"

var configTemplate, configTemplateErr = template.New("config").Parse(`
globals {
  daemonize = no
  setuid = false
  debug_level = 2
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

// The Harness provides an unique instance of gmond suitable for use in
// testing. Use NewHarness to create a Harness and start the gmond server, and
// ensure you 'defer h.Stop()' to shutdown the gmond server once the test has
// finished.
type Harness struct {
	Client      *gmetric.Client
	StopTimeout time.Duration
	t           *testing.T
	port        int
	configPath  string
	cmd         *exec.Cmd
}

func (h *Harness) start() {
	var err error
	if h.port == 0 {
		if h.port, err = freeport.Get(); err != nil {
			h.t.Fatal(err)
		}
	}

	cf, err := ioutil.TempFile("", "gmetric_test_gmond_conf")
	if err != nil {
		h.t.Fatal(err)
	}
	h.configPath = cf.Name()

	td := struct{ Port int }{Port: h.port}
	if err := configTemplate.Execute(cf, td); err != nil {
		h.t.Fatal(err)
	}

	if err := cf.Close(); err != nil {
		h.t.Fatal(err)
	}

	waiter := waitout.New(gmondServerStarted)
	h.cmd = exec.Command("gmond", "--conf", h.configPath)
	if os.Getenv("GMONDTEST_VERBOSE") == "1" {
		h.cmd.Stderr = io.MultiWriter(os.Stderr, waiter)
		h.cmd.Stdout = os.Stdout
	} else {
		h.cmd.Stderr = waiter
	}
	if err := h.cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	waiter.Wait()

	h.Client = &gmetric.Client{
		Addr: []net.Addr{
			&net.UDPAddr{IP: net.ParseIP(localhostIP), Port: h.port},
		},
	}

	if err := h.Client.Open(); err != nil {
		h.t.Fatal(err)
	}
}

// Stop the associated gmond server.
func (h *Harness) Stop() {
	fin := make(chan struct{})
	go func() {
		defer close(fin)
		if err := h.Client.Close(); err != nil {
			h.t.Fatal(err)
		}

		if err := h.cmd.Process.Kill(); err != nil {
			h.t.Fatal(err)
		}

		if err := os.Remove(h.configPath); err != nil {
			h.t.Fatal(err)
		}
	}()
	select {
	case <-fin:
	case <-time.After(h.StopTimeout):
	}
}

// Returns the current state of the associated gmond instance.
func (h *Harness) State() *gmon.Ganglia {
	addr := fmt.Sprintf("%s:%d", localhostIP, h.port)
	ganglia, err := gmon.RemoteRead("tcp", addr)
	if err != nil {
		h.t.Fatal(err)
	}
	return ganglia
}

// Check if associated gmond instance contains the given metric.
func (h *Harness) ContainsMetric(m *gmon.Metric) {
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
			h.t.Fatalf("did not find metric\n%s\n--in--\n%s", spew.Sdump(m), spew.Sdump(g))
		}
	}
}

// Create a new Harness. It will start the server and initialize the container
// Client.
func NewHarness(t *testing.T) *Harness {
	for {
		h := &Harness{
			t:           t,
			StopTimeout: 15 * time.Second,
		}
		start := make(chan struct{})
		go func() {
			defer close(start)
			h.start()
		}()
		select {
		case <-start:
			return h
		case <-time.After(5 * time.Second):
		}
	}
}
