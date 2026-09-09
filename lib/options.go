package lib

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
)

// Options contains all of the gowitness options
type Options struct {
	// Logging
	Logger         *zerolog.Logger
	Debug          bool
	DisableLogging bool

	// Screenshots
	ScreenshotPath string

	// Generic options
	Threads    int
	NoHTTPS    bool
	NoHTTP     bool
	ServerAddr string
	BasePath   string

	// Server command
	AllowInsecureURIs bool

	// File command
	File string

	// Scan command
	ScanCidr     []string
	ScanCidrFile string
	ScanRandom   bool
	ScanPorts    string
	PortsSmall   bool
	PortsMedium  bool
	PortsLarge   bool

	// Single
	ScreenshotFileName string

	// Nessus
	NessusPluginContains []string
	NessusServiceNames   []string
	NessusPluginOutput   []string
	NessusPorts          []int

	// Nmap
	NmapFile            string
	NmapService         []string
	NmapServiceContains string
	NmapPorts           []int
	NmapScanHostnames   bool
	NmapOpenPortsOnly   bool

	// Report List
	ReportJSON     bool
	ReportCSV      bool
	PerceptionSort bool

	// Merge
	MergeDBs        []string
	MergeSourcePath string
	MergeOutputDB   string
}

// NewOptions returns a new options struct
func NewOptions() *Options {
	return &Options{}
}

// ThreadCount is the number of concurrent workers to use, guarding against
// values that would otherwise stall the worker pool entirely.
func (opt *Options) ThreadCount() int {
	if opt.Threads < 1 {
		return 1
	}

	return opt.Threads
}

// PrepareScreenshotPath makes sure the screenshot directory exists and that we
// can actually write to it. The write probe matters because the directory is
// often a mount shared with the container (which runs as nobody), in which case
// creation succeeds but every screenshot write later fails.
func (opt *Options) PrepareScreenshotPath() error {

	if err := os.MkdirAll(opt.ScreenshotPath, 0750); err != nil {
		return fmt.Errorf("could not create screenshot directory %s: %w", opt.ScreenshotPath, err)
	}

	probe, err := os.CreateTemp(opt.ScreenshotPath, ".gowitness-write-probe-*")
	if err != nil {
		return fmt.Errorf("screenshot directory %s is not writable: %w", opt.ScreenshotPath, err)
	}
	probe.Close()

	return os.Remove(probe.Name())
}
