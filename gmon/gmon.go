// Package gmon provides read access to the gmon data.
package gmon

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"net"
)

// ExtraElement is one extra on a metric.
type ExtraElement struct {
	Name string `xml:"NAME,attr"`
	Val  string `xml:"VAL,attr"`
}

// ExtraData is the set extras on a metric.
type ExtraData struct {
	ExtraElements []ExtraElement `xml:"EXTRA_ELEMENT"`
}

// Metric as returned by gmond.
type Metric struct {
	Name      string    `xml:"NAME,attr"`
	Value     string    `xml:"VAL,attr"`
	Unit      string    `xml:"UNITS,attr"`
	Slope     string    `xml:"SLOPE,attr"`
	Tn        int       `xml:"TN,attr"`
	Tmax      int       `xml:"TMAX,attr"`
	Dmax      int       `xml:"DMAX,attr"`
	ExtraData ExtraData `xml:"EXTRA_DATA"`
}

// Host as returned by gmon.
type Host struct {
	Name         string   `xml:"NAME,attr"`
	IP           string   `xml:"IP,attr"`
	Tags         string   `xml:"TAGS,attr"`
	Reported     int      `xml:"REPORTED,attr"`
	Tn           int      `xml:"TN,attr"`
	Tmax         int      `xml:"TMAX,attr"`
	Dmax         int      `xml:"DMAX,attr"`
	Location     string   `xml:"LOCATION,attr"`
	GmondStarted int      `xml:"GMOND_STARTED,attr"`
	Metrics      []Metric `xml:"METRIC"`
}

// Cluster as returned by gmon.
type Cluster struct {
	Name      string `xml:"NAME,attr"`
	Owner     string `xml:"OWNER,attr"`
	LatLong   string `xml:"LATLONG,attr"`
	URL       string `xml:"URL,attr"`
	Localtime int    `xml:"LOCALTIME,attr"`
	Hosts     []Host `xml:"HOST"`
}

// Ganglia is the root document returned by gmon.
type Ganglia struct {
	XMLNAME  xml.Name  `xml:"GANGLIA_XML"`
	Clusters []Cluster `xml:"CLUSTER"`
}

// Read the gmond XML output.
func Read(r io.Reader) (*Ganglia, error) {
	ganglia := Ganglia{}
	decoder := xml.NewDecoder(r)
	decoder.CharsetReader = charsetReader
	if err := decoder.Decode(&ganglia); err != nil {
		return nil, err
	}
	return &ganglia, nil
}

// RemoteRead will connect to the given network/address and read from it.
func RemoteRead(network, addr string) (*Ganglia, error) {
	c, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return Read(bufio.NewReader(c))
}

func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	if charset != "ISO-8859-1" {
		return nil, fmt.Errorf("unsupported charset %s", charset)
	}
	return input, nil
}
