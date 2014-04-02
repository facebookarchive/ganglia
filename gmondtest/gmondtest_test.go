package gmondtest_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/facebookgo/ganglia/gmetric"
	"github.com/facebookgo/ganglia/gmon"
	"github.com/facebookgo/ganglia/gmondtest"
)

func TestCanSend(t *testing.T) {
	t.Parallel()
	h := gmondtest.NewHarness(t)
	defer h.Stop()

	m := &gmetric.Metric{
		Name:         "uint8_metric",
		Host:         "localhost",
		ValueType:    gmetric.ValueUint8,
		Units:        "count",
		Slope:        gmetric.SlopeBoth,
		TickInterval: 20 * time.Second,
		Lifetime:     24 * time.Hour,
	}
	const val = 10

	if err := h.Client.WriteMeta(m); err != nil {
		t.Fatal(err)
	}

	if err := h.Client.WriteValue(m, val); err != nil {
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
