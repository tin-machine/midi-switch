package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MIDI struct {
		PortName string `yaml:"port_name"`
		Port     string `yaml:"port"`
	} `yaml:"midi"`

	Defaults struct {
		CooldownMS        int `yaml:"cooldown_ms"`
		CommandTimeoutSec int `yaml:"command_timeout_sec"`
		DDCThrottleMS     int `yaml:"ddc_throttle_ms"`
	} `yaml:"defaults"`

	Actions []Action `yaml:"actions"`
}

type Action struct {
	Name    string   `yaml:"name"`
	Event   Event    `yaml:"event"`
	Command []string `yaml:"command"`
	DDC     *DDC     `yaml:"ddc"`
	Volume  *Volume  `yaml:"volume"`
}

type DDC struct {
	Bus           int    `yaml:"bus"`
	VCP           string `yaml:"vcp"`
	Value         string `yaml:"value"`
	NoVerify      bool   `yaml:"noverify"`
	ScaleFromMIDI bool   `yaml:"scale_from_midi"`
	Min           int    `yaml:"min"`
	Max           int    `yaml:"max"`
	ThrottleMS    int    `yaml:"throttle_ms"`
}

type Volume struct {
	Target     string `yaml:"target"`
	Min        int    `yaml:"min"`
	Max        int    `yaml:"max"`
	ThrottleMS int    `yaml:"throttle_ms"`
}

type Event struct {
	Type        string `yaml:"type"`
	Channel     *int   `yaml:"channel"`
	Note        int    `yaml:"note"`
	VelocityMin int    `yaml:"velocity_min"`
	Controller  int    `yaml:"controller"`
	ValueMin    int    `yaml:"value_min"`
	ValueMax    int    `yaml:"value_max"`
}

type MIDIEvent struct {
	Type       string
	Channel    int
	Note       int
	Velocity   int
	Controller int
	Value      int
}

var (
	noteOnRe     = regexp.MustCompile(`Note on\s+(\d+), note (\d+), velocity (\d+)`)
	ccRe         = regexp.MustCompile(`Control change\s+(\d+), controller (\d+), value (\d+)`)
	clientRe     = regexp.MustCompile(`^client\s+(\d+):\s+'([^']+)'`)
	portRe       = regexp.MustCompile(`^\s+(\d+)\s+'([^']+)'`)
	lastRun      = map[string]time.Time{}
	actionMu     sync.Mutex
	sliderMu     sync.Mutex
	sliders      = map[string]*sliderState{}
	aconnectPath = "aconnect"
)

type sliderState struct {
	timer *time.Timer
	value int
}

func main() {
	configPath := flag.String("config", "config.example.yaml", "config file path")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	applyDefaults(cfg)

	port, err := resolveMIDIPort(cfg)
	if err != nil {
		log.Fatalf("failed to resolve MIDI port: %v", err)
	}

	log.Printf("starting midi-switch")
	log.Printf("midi port: %s", port)

	cmd := exec.Command("aseqdump", "-p", port)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("failed to get stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("failed to start aseqdump: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		ev, ok := parseMIDIEvent(line)
		if !ok {
			continue
		}

		log.Printf("midi event: %+v", ev)
		for _, action := range cfg.Actions {
			if matchAction(action, ev) {
				if err := handleAction(cfg, action, ev); err != nil {
					log.Printf("action failed: name=%s err=%v", action.Name, err)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("scanner error: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		log.Fatalf("aseqdump exited: %v", err)
	}
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Defaults.CooldownMS <= 0 {
		cfg.Defaults.CooldownMS = 700
	}
	if cfg.Defaults.CommandTimeoutSec <= 0 {
		cfg.Defaults.CommandTimeoutSec = 5
	}
	if cfg.Defaults.DDCThrottleMS <= 0 {
		cfg.Defaults.DDCThrottleMS = 400
	}
}

func resolveMIDIPort(cfg *Config) (string, error) {
	if cfg.MIDI.PortName != "" {
		port, err := findALSASequencerPort(cfg.MIDI.PortName)
		if err == nil {
			return port, nil
		}
		log.Printf("midi port_name not resolved: name=%q err=%v", cfg.MIDI.PortName, err)
	}

	if cfg.MIDI.Port != "" {
		return cfg.MIDI.Port, nil
	}
	return "", errors.New("midi.port_name or midi.port is required")
}

func findALSASequencerPort(name string) (string, error) {
	out, err := exec.Command(aconnectPath, "-l").Output()
	if err != nil {
		return "", err
	}

	var currentClient string
	var currentClientName string
	for _, line := range strings.Split(string(out), "\n") {
		if m := clientRe.FindStringSubmatch(line); m != nil {
			currentClient = m[1]
			currentClientName = m[2]
			continue
		}

		m := portRe.FindStringSubmatch(line)
		if m == nil || currentClient == "" {
			continue
		}

		portName := m[2]
		if strings.Contains(currentClientName, name) || strings.Contains(portName, name) {
			return currentClient + ":" + m[1], nil
		}
	}

	return "", fmt.Errorf("ALSA sequencer port containing %q not found", name)
}

func parseMIDIEvent(line string) (MIDIEvent, bool) {
	if m := noteOnRe.FindStringSubmatch(line); m != nil {
		ch, _ := strconv.Atoi(m[1])
		note, _ := strconv.Atoi(m[2])
		velocity, _ := strconv.Atoi(m[3])
		return MIDIEvent{Type: "note_on", Channel: ch, Note: note, Velocity: velocity}, true
	}

	if m := ccRe.FindStringSubmatch(line); m != nil {
		ch, _ := strconv.Atoi(m[1])
		controller, _ := strconv.Atoi(m[2])
		value, _ := strconv.Atoi(m[3])
		return MIDIEvent{Type: "control_change", Channel: ch, Controller: controller, Value: value}, true
	}

	return MIDIEvent{}, false
}

func matchAction(action Action, ev MIDIEvent) bool {
	if action.Event.Type != ev.Type {
		return false
	}
	if action.Event.Channel != nil && *action.Event.Channel != ev.Channel {
		return false
	}

	switch ev.Type {
	case "note_on":
		return action.Event.Note == ev.Note && ev.Velocity >= action.Event.VelocityMin
	case "control_change":
		if action.Event.Controller != ev.Controller {
			return false
		}
		if action.Event.ValueMin != 0 && ev.Value < action.Event.ValueMin {
			return false
		}
		if action.Event.ValueMax != 0 && ev.Value > action.Event.ValueMax {
			return false
		}
		return true
	default:
		return false
	}
}

func handleAction(cfg *Config, action Action, ev MIDIEvent) error {
	if action.DDC != nil {
		if action.DDC.ScaleFromMIDI {
			scheduleDDCAction(cfg, action, ev)
			return nil
		}
		return runDDCAction(cfg, action, ev)
	}
	if action.Volume != nil {
		scheduleVolumeAction(cfg, action, ev)
		return nil
	}
	return runCommandAction(cfg, action)
}

func runCommandAction(cfg *Config, action Action) error {
	return runCommand(cfg, action, true)
}

func runCommand(cfg *Config, action Action, useCooldown bool) error {
	if len(action.Command) == 0 {
		return errors.New("empty command and no ddc action")
	}

	actionMu.Lock()
	defer actionMu.Unlock()

	if useCooldown {
		now := time.Now()
		cooldown := time.Duration(cfg.Defaults.CooldownMS) * time.Millisecond
		if prev, ok := lastRun[action.Name]; ok && now.Sub(prev) < cooldown {
			log.Printf("skip action due to cooldown: %s", action.Name)
			return nil
		}
		lastRun[action.Name] = now
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Defaults.CommandTimeoutSec)*time.Second)
	defer cancel()

	log.Printf("run action: %s command=%v", action.Name, action.Command)
	cmd := exec.CommandContext(ctx, action.Command[0], action.Command[1:]...)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log.Printf("command output: %s", strings.TrimSpace(string(out)))
	}
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("command timeout: %s", action.Name)
	}
	if err != nil {
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}

func scheduleDDCAction(cfg *Config, action Action, ev MIDIEvent) {
	value := scaleMIDIValue(ev.Value, action.DDC.Min, action.DDC.Max)
	throttle := action.DDC.ThrottleMS
	if throttle <= 0 {
		throttle = cfg.Defaults.DDCThrottleMS
	}

	sliderMu.Lock()
	state := sliders[action.Name]
	if state == nil {
		state = &sliderState{}
		sliders[action.Name] = state
	}
	state.value = value
	if state.timer == nil {
		state.timer = time.AfterFunc(time.Duration(throttle)*time.Millisecond, func() {
			for {
				sliderMu.Lock()
				latest := state.value
				state.timer = nil
				sliderMu.Unlock()

				if err := runDDCValue(cfg, action, latest); err != nil {
					log.Printf("ddc action failed: name=%s err=%v", action.Name, err)
				}

				sliderMu.Lock()
				if state.value == latest {
					sliderMu.Unlock()
					return
				}
				state.timer = time.NewTimer(time.Duration(throttle) * time.Millisecond)
				timer := state.timer
				sliderMu.Unlock()
				<-timer.C
			}
		})
	}
	sliderMu.Unlock()
}

func runDDCAction(cfg *Config, action Action, ev MIDIEvent) error {
	if action.DDC == nil {
		return errors.New("ddc action is nil")
	}

	value := 0
	if action.DDC.ScaleFromMIDI {
		value = scaleMIDIValue(ev.Value, action.DDC.Min, action.DDC.Max)
		return runDDCValue(cfg, action, value)
	}
	if action.DDC.Value == "" {
		return errors.New("ddc.value is required")
	}
	return runDDCCommand(cfg, action, action.DDC.Value)
}

func runDDCValue(cfg *Config, action Action, value int) error {
	return runDDCCommand(cfg, action, strconv.Itoa(value))
}

func runDDCCommand(cfg *Config, action Action, value string) error {
	if action.DDC.Bus <= 0 {
		return errors.New("ddc.bus is required")
	}
	if action.DDC.VCP == "" {
		return errors.New("ddc.vcp is required")
	}

	command := []string{
		"ddcutil",
		"--bus",
		strconv.Itoa(action.DDC.Bus),
	}
	if action.DDC.NoVerify {
		command = append(command, "--noverify")
	}
	command = append(command, "setvcp", action.DDC.VCP, value)

	return runCommand(cfg, Action{
		Name:    action.Name,
		Command: command,
	}, !action.DDC.ScaleFromMIDI)
}

func scheduleVolumeAction(cfg *Config, action Action, ev MIDIEvent) {
	value := scaleMIDIValue(ev.Value, action.Volume.Min, action.Volume.Max)
	throttle := action.Volume.ThrottleMS
	if throttle <= 0 {
		throttle = cfg.Defaults.DDCThrottleMS
	}

	sliderMu.Lock()
	state := sliders[action.Name]
	if state == nil {
		state = &sliderState{}
		sliders[action.Name] = state
	}
	state.value = value
	if state.timer == nil {
		state.timer = time.AfterFunc(time.Duration(throttle)*time.Millisecond, func() {
			for {
				sliderMu.Lock()
				latest := state.value
				state.timer = nil
				sliderMu.Unlock()

				if err := runVolumeValue(cfg, action, latest); err != nil {
					log.Printf("volume action failed: name=%s err=%v", action.Name, err)
				}

				sliderMu.Lock()
				if state.value == latest {
					sliderMu.Unlock()
					return
				}
				state.timer = time.NewTimer(time.Duration(throttle) * time.Millisecond)
				timer := state.timer
				sliderMu.Unlock()
				<-timer.C
			}
		})
	}
	sliderMu.Unlock()
}

func runVolumeValue(cfg *Config, action Action, value int) error {
	target := action.Volume.Target
	if target == "" {
		target = "@DEFAULT_SINK@"
	}
	command := []string{
		"pactl",
		"set-sink-volume",
		target,
		strconv.Itoa(value) + "%",
	}
	return runCommand(cfg, Action{
		Name:    action.Name,
		Command: command,
	}, false)
}

func scaleMIDIValue(value, minValue, maxValue int) int {
	if minValue == 0 && maxValue == 0 {
		maxValue = 100
	}
	if maxValue < minValue {
		maxValue = minValue
	}
	if value < 0 {
		value = 0
	}
	if value > 127 {
		value = 127
	}
	return minValue + ((value*(maxValue-minValue) + 63) / 127)
}
