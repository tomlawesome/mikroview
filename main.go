package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tomlawesome/mikroview/internal/api"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/geoip"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/syslog"
	"github.com/tomlawesome/mikroview/web"
)

// globalSpikeCheckInterval is how often the global volume-spike detector
// re-samples the store's current events-per-second figure. Independent
// of STATS_REFRESH_MS on the frontend -- this only needs to be frequent
// enough for the detector's own EMA baseline to track real trends, not
// to feel "live" to a person.
const globalSpikeCheckInterval = 10 * time.Second

func main() {
	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), os.Args[1:])
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	st := store.New(cfg.Store.MaxEvents, cfg.Store.Retention)
	devices := device.NewRegistry(cfg.Devices)
	h := hub.New()
	geo, err := geoip.Open(cfg.GeoIP.DBPath)
	if err != nil {
		log.Printf("geoip: %v (country flags disabled)", err)
	}
	defer geo.Close()
	rep := reputation.New(cfg.Reputation.AbuseIPDBKey)

	fs, err := flags.Open(cfg.Flags.StorePath)
	if err != nil {
		log.Printf("flags: %v (continuing with in-memory-only flag state)", err)
	}
	detectCfg := detect.Config{
		PortScanThreshold:      cfg.Flags.PortScanThreshold,
		PortScanWindow:         cfg.Flags.PortScanWindow,
		ActivitySpikeThreshold: cfg.Flags.ActivitySpikeThreshold,
		ActivitySpikeWindow:    cfg.Flags.ActivitySpikeWindow,
		CriticalPorts:          cfg.Flags.CriticalPorts,
		CriticalPortThreshold:  cfg.Flags.CriticalPortThreshold,
		CriticalPortWindow:     cfg.Flags.CriticalPortWindow,
		GlobalSpikeMultiplier:  cfg.Flags.GlobalSpikeMultiplier,
		GlobalSpikeMinEPS:      cfg.Flags.GlobalSpikeMinEPS,

		DistributedBruteForceThreshold: cfg.Flags.DistributedBruteForceThreshold,
		DistributedBruteForceWindow:    cfg.Flags.DistributedBruteForceWindow,

		OutboundAnomalyThreshold: cfg.Flags.OutboundAnomalyThreshold,
		OutboundAnomalyWindow:    cfg.Flags.OutboundAnomalyWindow,

		InternalReconThreshold: cfg.Flags.InternalReconThreshold,
		InternalReconWindow:    cfg.Flags.InternalReconWindow,

		RuleSpikeMultiplier: cfg.Flags.RuleSpikeMultiplier,
		RuleSpikeMinRate:    cfg.Flags.RuleSpikeMinRate,
		RuleSpikeWindow:     cfg.Flags.RuleSpikeWindow,

		RepeatedDropsThreshold: cfg.Flags.RepeatedDropsThreshold,
		RepeatedDropsWindow:    cfg.Flags.RepeatedDropsWindow,
	}
	detector := detect.New(detectCfg, fs)
	globalSpike := detect.NewGlobalSpikeDetector(detectCfg, fs)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	raw := make(chan syslog.RawMessage, 4096)

	go func() {
		if err := syslog.ListenUDP(ctx, cfg.Listen.SyslogUDP, raw); err != nil && ctx.Err() == nil {
			log.Fatalf("syslog udp listener: %v", err)
		}
	}()
	go func() {
		if err := syslog.ListenTCP(ctx, cfg.Listen.SyslogTCP, raw); err != nil && ctx.Err() == nil {
			log.Fatalf("syslog tcp listener: %v", err)
		}
	}()

	go ingest(ctx, raw, st, devices, h, geo, detector)

	go func() {
		ticker := time.NewTicker(globalSpikeCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				globalSpike.Check(st.Stats().EventsPerSecond, time.Now())
			}
		}
	}()

	srv := &api.Server{Store: st, Devices: devices, Hub: h, Reputation: rep, Flags: fs, StartTime: time.Now()}

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/", srv.Routes())
	if frontend, err := web.DistFS(); err != nil {
		log.Printf("frontend: %v (serving API only)", err)
	} else {
		rootMux.Handle("/", http.FileServer(http.FS(frontend)))
	}

	httpServer := &http.Server{
		Addr:    cfg.Listen.HTTP,
		Handler: rootMux,
		// Bounds a slow client trickling headers/body in to tie up a
		// connection indefinitely (the WS listener, syslog listeners, and
		// hub already have their own backpressure/deadline handling --
		// this was the one place in the request path without any). None
		// of these continue to apply once /api/ws hijacks a connection:
		// the WS handler manages its own read/write deadlines from that
		// point on (see internal/api/ws.go), and Go's server stops
		// enforcing these on a connection once it's been hijacked.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("mikroview: http on %s, syslog udp/tcp on %s/%s", cfg.Listen.HTTP, cfg.Listen.SyslogUDP, cfg.Listen.SyslogTCP)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server: %v", err)
	}
}

// ingest is the single store-writer goroutine: it parses each raw syslog
// message, resolves device identity, inserts the resulting Event into the
// store, and hands the stored (ID-assigned) event to the hub for
// broadcast. Keeping this on one goroutine means Store and the device
// Registry never need to arbitrate concurrent writers.
func ingest(ctx context.Context, raw <-chan syslog.RawMessage, st *store.Store, devices *device.Registry, h *hub.Hub, geo *geoip.Lookup, detector *detect.Detector) {
	for {
		select {
		case <-ctx.Done():
			return
		case rm := <-raw:
			env := syslog.ParseEnvelope(rm.Data, rm.RecvTime)
			parsed := routeros.Parse(env.Message)
			deviceID := devices.Resolve(rm.SourceIP, rm.RecvTime)
			srcCountry, _ := geo.Country(parsed.SrcIP)
			dstCountry, _ := geo.Country(parsed.DstIP)

			e := store.Event{
				Time:         env.Timestamp,
				DeviceID:     deviceID,
				SourceIP:     rm.SourceIP,
				Action:       parsed.Action,
				RuleLabel:    parsed.RuleLabel,
				Chain:        parsed.Chain,
				InInterface:  parsed.InInterface,
				OutInterface: parsed.OutInterface,
				ConnState:    parsed.ConnState,
				Protocol:     parsed.Protocol,
				SrcMAC:       parsed.SrcMAC,
				SrcIP:        parsed.SrcIP,
				SrcPort:      parsed.SrcPort,
				DstIP:        parsed.DstIP,
				DstPort:      parsed.DstPort,
				SrcCountry:   srcCountry,
				DstCountry:   dstCountry,
				NatIP:        parsed.NatIP,
				NatPort:      parsed.NatPort,
				NatRaw:       parsed.NatRaw,
				Length:       parsed.Length,
				Flags:        parsed.Flags,
				Raw:          parsed.Raw,
			}

			stored := st.Insert(e)
			h.Broadcast(stored)
			detector.Observe(stored)
		}
	}
}
