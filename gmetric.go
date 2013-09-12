// Package gmetric provides a client for the ganglia gmetric API.
package gmetric

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"time"
)

var zeroByte = []byte{byte(0)}

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
func (v valueType) encode(w io.Writer, val interface{}) error {
	switch v {
	default:
		writeString(w, fmt.Sprint(val))
	case ValueUint8, ValueInt8, ValueUint16, ValueInt16, ValueUint32, ValueInt32:
		writeString(w, fmt.Sprintf("%d", val))
	case ValueFloat32, ValueFloat64:
		writeString(w, fmt.Sprintf("%f", val))
	}
	return nil
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

// A Client represents a set of connections to write metrics to.
type Client struct {
	Addr   []*net.UDPAddr
	conn   []*net.UDPConn
	writer io.Writer
}

// Defines a Metric.
type Metric struct {
	Name         string
	Title        string
	Description  string
	Group        string
	Units        string
	Host         string
	Spoof        string
	ValueType    valueType
	Slope        slopeType
	TickInterval time.Duration // Also known as TMax.
	Lifetime     time.Duration // Also known as DMax.
}

// Writes a metadata packet for the Metric.
func (m *Metric) EncodeMeta(w io.Writer) error {
	writeUint32(w, 128)
	m.writeHead(w)
	writeString(w, m.ValueType.Type())
	writeString(w, m.Name)
	writeString(w, m.Units)
	writeUint32(w, uint32(m.Slope))
	writeUint32(w, uint32(m.TickInterval.Seconds()))
	writeUint32(w, uint32(m.Lifetime.Seconds()))

	var pairs [][2]string
	if m.Title != "" {
		pairs = append(pairs, [2]string{"TITLE", m.Title})
	}
	if m.Description != "" {
		pairs = append(pairs, [2]string{"DESC", m.Description})
	}
	if m.Spoof != "" {
		pairs = append(pairs, [2]string{"SPOOF_HOST", m.Spoof})
	}
	if m.Group != "" {
		pairs = append(pairs, [2]string{"GROUP", m.Group})
	}
	writePairs(w, pairs)
	return nil
}

// Writes a value packet for the given value. The value will be encoded based
// on the configured ValueType.
func (m *Metric) EncodeValue(w io.Writer, val interface{}) error {
	writeUint32(w, 133)
	m.writeHead(w)
	writeString(w, "%s")
	return m.ValueType.encode(w, val)
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
	c.writer.Write(buf.Bytes())
	return nil
}

// Send a value for the Metric.
func (c *Client) SendValue(m *Metric, val interface{}) error {
	var buf bytes.Buffer
	if err := m.EncodeValue(&buf, val); err != nil {
		return err
	}
	c.writer.Write(buf.Bytes())
	return nil
}

// Start the client and establish the connections. If an error is returned it
// will be a MultiError.
func (c *Client) Start() error {
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
	c.writer = io.MultiWriter(writers...)

	if len(errs) == 0 {
		return nil
	}
	return errs
}

// Shutdown the client and close the connections. If an error is returned it
// will be a MultiError.
func (c *Client) Stop() error {
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

func writePairs(w io.Writer, pairs [][2]string) {
	writeUint32(w, uint32(len(pairs)))
	for _, p := range pairs {
		writeString(w, p[0])
		writeString(w, p[1])
	}
}
