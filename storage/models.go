package storage

import (
	"strconv"
	"time"

	"gorm.io/gorm"
)

// URL contains information about a URL
type URL struct {
	gorm.Model

	URL            string
	FinalURL       string
	ResponseCode   int
	ResponseReason string
	Proto          string
	ContentLength  int64
	Title          string
	Filename       string
	IsPDF          bool
	PerceptionHash string // Compare 2 image
	DOM            string
	Screenshot     string
	Callback       string
	IdUrl          int
	NilsimsaHash   string // Compare 2 DOM
	SameSite       int    // if 0 is not check else if -1 is not same any site, esle = IdUrl
	Hidden         bool   `gorm:"default:false"`

	TLS TLS

	Headers      []Header
	Technologies []Technologie
	Console      []ConsoleLog
	Network      []NetworkLog
}

// AddHeader adds a new header to a URL
func (url *URL) AddHeader(key string, value string) {
	url.Headers = append(url.Headers, Header{
		Key:   key,
		Value: value,
	})
}

// AddTechnlogies adds a new technologies to a URL
func (url *URL) AddTechnologie(value string) {
	url.Technologies = append(url.Technologies, Technologie{
		Value: value,
	})
}

// MarshallCSV returns values as a slice
func (url *URL) MarshallCSV() (res []string) {
	return []string{url.URL,
		url.FinalURL,
		strconv.Itoa(url.ResponseCode),
		url.ResponseReason,
		url.Proto,
		strconv.Itoa(int(url.ContentLength)),
		url.Title,
		url.Filename}
}

// MarshalJSON returns JSON encoding of url. Implements json.Marshaler.
// func (url *URL) MarshallJSON() ([]byte, error) {
// 	var tmp struct {
// 		URL            string `json:"url"`
// 		FinalURL       string `json:"final_url"`
// 		ResponseCode   int    `json:"response_code"`
// 		ResponseReason string `json:"response_reason"`
// 		Proto          string `json:"proto"`
// 		ContentLength  int64  `json:"content_length"`
// 		Title          string `json:"title"`
// 		Filename       string `json:"file_name"`
// 	}

// 	tmp.URL = url.URL
// 	tmp.FinalURL = url.FinalURL
// 	tmp.ResponseCode = url.ResponseCode
// 	tmp.ResponseReason = url.ResponseReason
// 	tmp.Proto = url.Proto
// 	tmp.ContentLength = url.ContentLength
// 	tmp.Title = url.Title
// 	tmp.Filename = url.Filename

// 	return json.Marshal(&tmp)
// }

// Header contains an HTTP header
type Header struct {
	gorm.Model

	URLID uint

	Key   string
	Value string
}

// Technologie contains a technologie
type Technologie struct {
	gorm.Model

	URLID uint

	Value string
}

// TLS contains TLS information for a URL
type TLS struct {
	gorm.Model

	URLID uint

	Version         uint16
	ServerName      string
	TLSCertificates []TLSCertificate
}

// TLSCertificate contain TLS Certificate information
type TLSCertificate struct {
	gorm.Model

	TLSID uint

	Raw                []byte
	DNSNames           []TLSCertificateDNSName
	SubjectCommonName  string
	IssuerCommonName   string
	SignatureAlgorithm string
	PubkeyAlgorithm    string
}

// AddDNSName adds a new DNS Name to a Certificate
func (tlsCert *TLSCertificate) AddDNSName(name string) {
	tlsCert.DNSNames = append(tlsCert.DNSNames, TLSCertificateDNSName{Name: name})
}

// TLSCertificateDNSName has DNS names for a TLS certificate
type TLSCertificateDNSName struct {
	gorm.Model

	TLSCertificateID uint
	Name             string
}

// ConsoleLog contains the console log, and exceptions emitted
type ConsoleLog struct {
	gorm.Model

	URLID uint

	Time  time.Time
	Type  string
	Value string
}

// RequestType are network log types
type RequestType int

const (
	HTTP RequestType = 0
	WS
)

// NetworkLog contains Chrome networks events that were emitted
type NetworkLog struct {
	gorm.Model

	URLID uint

	RequestID   string
	RequestType RequestType
	StatusCode  int64
	URL         string
	FinalURL    string
	IP          string
	Time        time.Time
	Error       string
}

// Queue job states, persisted in the ScreenshotQueue PID column.
const (
	QueueFailed  = -1 // job errored, ErrorMsg holds the reason
	QueuePending = 0  // waiting to be picked up by a worker
	QueueRunning = 1  // claimed by a worker, in flight
	QueueDone    = 2  // completed successfully
)

type ScreenshotQueue struct {
	gorm.Model

	URL      string
	PID      int    `gorm:"index"`
	ErrorMsg string `gorm:"default:''"`
	Callback string
	IdUrl    int `gorm:"default:0"`
}

type ConfigMachine struct {
	gorm.Model

	Key     string
	Machine string `gorm:"uniqueIndex"`
	Value   string
}
