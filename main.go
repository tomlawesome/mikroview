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
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/geoip"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/syslog"
	"github.com/tomlawesome/mikroview/web"
)

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

	go ingest(ctx, raw, st, devices, h, geo)

	srv := &api.Server{Store: st, Devices: devices, Hub: h, StartTime: time.Now()}

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/", srv.Routes())
	if frontend, err := web.DistFS(); err != nil {
		log.Printf("frontend: %v (serving API only)", err)
	} else {
		rootMux.Handle("/", http.FileServer(http.FS(frontend)))
	}

	httpServer := &http.Server{Addr: cfg.Listen.HTTP, Handler: rootMux}

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
func ingest(ctx context.Context, raw <-chan syslog.RawMessage, st *store.Store, devices *device.Registry, h *hub.Hub, geo *geoip.Lookup) {
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
		}
	}
}
