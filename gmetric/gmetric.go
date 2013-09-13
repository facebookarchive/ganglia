// Package gmetric provides a client for the ganglia gmetric API.
package gmetric

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

var (
	zeroByte   = []byte{byte(0)}
	errNoAddrs = errors.New("gmetric: no addrs provided")
)

type slopeType uint32

// The slope types supported by Ganglia.
const (
	SlopeZero slopeType = iota
	SlopePositive
	SlopeNegative
	SlopeBoth
	SlopeUnspecified
)

type valueType uint

// The value types supported by Ganglia.
const (
	ValueString valueType = iota + 1
	ValueUint8
	ValueInt8
	ValueUint16
	ValueInt16
	ValueUint32
	ValueInt32
	ValueFloat32
	ValueFloat64
)

// Type string per configured type.
func (v valueType) Type() string {
	switch v {
	case ValueString:
		return "string"
	case ValueUint8:
		return "uint8"
	case ValueInt8:
		return "int8"
	case ValueUint16:
		return "uint16"
	case ValueInt16:
		return "int16"
	case ValueUint32:
		return "uint32"
	case ValueInt32:
		return "int32"
	case ValueFloat32:
		return "float"
	case ValueFloat64:
		return "double"
	}
	return "unknown"
}

// Encode a value.
func (v valueType) encode(w io.Writer, val interface{}) {
	switch v {
	default:
		writeString(w, fmt.Sprint(val))
	case ValueUint8, ValueInt8, ValueUint16, ValueInt16, ValueUint32, ValueInt32:
		writeString(w, fmt.Sprintf("%d", val))
	case ValueFloat32, ValueFloat64:
		writeString(w, fmt.Sprintf("%f", val))
	}
}

// Represents a collection of errors.
type MultiError []error

// Returns a concatenation of all the contained errors.
func (m MultiError) Error() string {
	var buf bytes.Buffer
	buf.WriteString("gmetric: multi-error:")
	for _, e := range m {
		buf.WriteRune('\n')
		buf.WriteString(e.Error())
	}
	return buf.String()
}

// A Client represents a set of connections to write metrics to. The Client is
// itself a Writer which writes the given bytes to all open connections.
type Client struct {
	io.Writer
	Addr []*net.UDPAddr
	conn []*net.UDPConn
}

// Defines a Metric.
type Metric struct {
	// The name is used as the file name, and also the title unless one is
	// explicitly provided.
	Name string

	// The title is for human consumption and is shown atop the graph.
	Title string

	// Descriptions serve as documentation.
	Description string

	// The group ensures your metric is kept alongside sibling metrics.
	Group string

	// The units are shown in the graph to provide context to the numbers.
	Units string

	// The actual hostname for the machine.
	Host string

	// Optional spoof name for the machine. Since the default is reverse DNS this
	// allows for overriding the hostname to make it useful.
	Spoof string

	// Defines the value type for this metric. This also controls how a given
	// value is encoded. You must specify one of the predefined constants.
	ValueType valueType

	// Defines the slope type. You must specify one of the predefined constants.
	Slope slopeType

	// Also known as TMax, it defines the max time interval between which the
	// daemon will expect updates. This should map to how often you publish the
	// metric.
	TickInterval time.Duration

	// Also known as DMax, it defines the lifetime for the metric. That is, once
	// the last received metric is older than the defined value it will become
	// eligible for garbage collection.
	Lifetime time.Duration
}

// Writes a metadata packet for the Metric.
func (m *Metric) EncodeMeta(w io.Writer) (err error) {
	pw := &panickyWriter{Writer: w}
	defer func() {
		if r := recover(); r != nil {
			if r == errPanickyWriter {
				err = pw.Error
			} else {
				panic(r)
			}
		}
	}()

	writeUint32(pw, 128)
	m.writeHead(pw)
	writeString(pw, m.ValueType.Type())
	writeString(pw, m.Name)
	writeString(pw, m.Units)
	writeUint32(pw, uint32(m.Slope))
	writeUint32(pw, uint32(m.TickInterval.Seconds()))
	writeUint32(pw, uint32(m.Lifetime.Seconds()))

	var extras [][2]string
	if m.Title != "" {
		extras = append(extras, [2]string{"TITLE", m.Title})
	}
	if m.Description != "" {
		extras = append(extras, [2]string{"DESC", m.Description})
	}
	if m.Spoof != "" {
		extras = append(extras, [2]string{"SPOOF_HOST", m.Spoof})
	}
	if m.Group != "" {
		extras = append(extras, [2]string{"GROUP", m.Group})
	}
	writeExtras(pw, extras)
	return
}

// Writes a value packet for the given value. The value will be encoded based
// on the configured ValueType.
func (m *Metric) EncodeValue(w io.Writer, val interface{}) (err error) {
	pw := &panickyWriter{Writer: w}
	defer func() {
		if r := recover(); r != nil {
			if r == errPanickyWriter {
				err = pw.Error
			} else {
				panic(r)
			}
		}
	}()

	writeUint32(pw, 133)
	m.writeHead(pw)
	writeString(pw, "%s")
	m.ValueType.encode(pw, val)
	return
}

func (m *Metric) writeHead(w io.Writer) {
	spoof := m.Spoof != ""
	if spoof {
		writeString(w, m.Spoof)
	} else {
		writeString(w, m.Host)
	}
	writeString(w, m.Name)
	if spoof {
		writeUint32(w, 1)
	} else {
		writeUint32(w, 0)
	}
}

// Send the Metric metadata.
func (c *Client) SendMeta(m *Metric) error {
	var buf bytes.Buffer
	if err := m.EncodeMeta(&buf); err != nil {
		return err
	}
	if _, err := c.Write(buf.Bytes()); err != nil {
		return err
	}
	return nil
}

// Send a value for the Metric.
func (c *Client) SendValue(m *Metric, val interface{}) error {
	var buf bytes.Buffer
	if err := m.EncodeValue(&buf, val); err != nil {
		return err
	}
	if _, err := c.Write(buf.Bytes()); err != nil {
		return err
	}
	return nil
}

// Open the connections. If an error is returned it will be a MultiError.
func (c *Client) Open() error {
	if len(c.Addr) == 0 {
		return errNoAddrs
	}

	var errs MultiError
	var writers []io.Writer
	for _, addr := range c.Addr {
		s, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c.conn = append(c.conn, s)
		writers = append(writers, s)
	}
	c.Writer = io.MultiWriter(writers...)

	if len(errs) == 0 {
		return nil
	}
	return errs
}

// Close the connections. If an error is returned it will be a MultiError.
func (c *Client) Close() error {
	if len(c.Addr) == 0 {
		return errNoAddrs
	}

	var errs MultiError
	for _, conn := range c.conn {
		if err := conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

func writeUint32(w io.Writer, val uint32) {
	w.Write([]byte{
		byte(val >> 24 & 0xff),
		byte(val >> 16 & 0xff),
		byte(val >> 8 & 0xff),
		byte(val & 0xff),
	})
}

func writeString(w io.Writer, val string) {
	l := uint32(len(val))
	writeUint32(w, l)
	fmt.Fprint(w, val)
	offset := l % 4
	if offset != 0 {
		for j := offset; j < 4; j++ {
			w.Write(zeroByte)
		}
	}
}

func writeExtras(w io.Writer, extras [][2]string) {
	writeUint32(w, uint32(len(extras)))
	for _, p := range extras {
		writeString(w, p[0])
		writeString(w, p[1])
	}
}

var errPanickyWriter = errors.New("panicky-writer sentinel")

// Panicky Writer provides a io.Writer that panics whenever the underlying
// writer returns an error. This allows for localized panic/recover for less
// verbose error handling internally. DO NOT expose this without a
// corresponding recover().
type panickyWriter struct {
	io.Writer
	Count int
	Error error
}

func (p *panickyWriter) Write(b []byte) (int, error) {
	n, err := p.Writer.Write(b)
	p.Count += n
	if err != nil {
		p.Error = err
		panic(errPanickyWriter)
	}
	return n, nil
}
