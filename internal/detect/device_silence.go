package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/flags"
)

// DeviceLister is the slice of *device.Registry DeviceSilenceDetector
// needs -- an interface (rather than a concrete *device.Registry field)
// purely so tests can supply a small fake instead of standing up a real
// registry. device.Registry itself satisfies this with no changes.
type DeviceLister interface {
	List() []device.Info
}

// DeviceSilenceDetector periodically compares every configured device's
// LastSeen against Config.DeviceStaleAfter, raising a TypeDeviceSilence
// flag for a device that should be sending logs but has gone quiet.
// Checked periodically (see main.go's ticker), not per-event, since
// "absence of events" -- unlike every other detector in this package --
// is not a property any single event carries; mirrors
// GlobalSpikeDetector's own ticker-based check for the same reason, just
// with the opposite condition (silence instead of a spike).
//
// Only devices with Configured == true are eligible: an auto-discovered
// source (one seen on the wire but never added to config.yaml) has no
// expected cadence to fall silent from. A device that has never sent a
// single event yet (LastSeen still zero) is also skipped here -- that's
// "never contacted," a distinct condition from "went quiet after being
// active," and firing on it would mean every freshly configured device
// alarms immediately on startup before it's ever had a chance to send
// anything. The fleet-health API surfaces that "never seen" state
// separately (see internal/api's handleDevices), just not as a flag.
type DeviceSilenceDetector struct {
	cfg      Config
	fs       *flags.Store
	settings *SettingsStore
	devices  DeviceLister
}

// NewDeviceSilenceDetectorWithSettings constructs a device-silence
// detector backed by a live, mutable SettingsStore -- same on/off
// control every other detector gets via NewWithSettings/
// NewGlobalSpikeDetectorWithSettings (issue #44).
func NewDeviceSilenceDetectorWithSettings(cfg Config, fs *flags.Store, settings *SettingsStore, devices DeviceLister) *DeviceSilenceDetector {
	return &DeviceSilenceDetector{cfg: cfg, fs: fs, settings: settings, devices: devices}
}

// Check raises or re-fires a TypeDeviceSilence flag for every configured
// device whose LastSeen is at least Config.DeviceStaleAfter in the past.
// Like every other detector, it never clears a flag itself once a device
// starts sending again -- that's left for a human to acknowledge via the
// flags UI, same lifecycle as every other flag type in this codebase.
func (d *DeviceSilenceDetector) Check(now time.Time) {
	if !d.settings.Get(DetectorDeviceSilence).Enabled {
		return
	}
	if d.cfg.DeviceStaleAfter <= 0 {
		// Same "off means off" contract as an unconfigured/zero threshold
		// anywhere else in this codebase -- 0 wouldn't mean "instantly
		// stale," it would just be a divide-by-nothing footgun, so treat
		// it as not configured.
		return
	}

	for _, info := range d.devices.List() {
		if !info.Configured || info.LastSeen.IsZero() {
			continue
		}
		elapsed := now.Sub(info.LastSeen)
		if elapsed < d.cfg.DeviceStaleAfter {
			continue
		}
		confidence := overshootConfidence(int(elapsed.Seconds()), int(d.cfg.DeviceStaleAfter.Seconds()))
		d.fs.AddWithConfidence(flags.TypeDeviceSilence, info.ID,
			fmt.Sprintf("%s has sent no syslog for %s, exceeding the %s staleness threshold",
				info.Name, elapsed.Round(time.Second), d.cfg.DeviceStaleAfter),
			confidence, now)
	}
}
