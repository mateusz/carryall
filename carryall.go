package main

import (
	"image/color"
	"math"
	"time"

	"github.com/faiface/pixel"
	"github.com/faiface/pixel/pixelgl"
	engine "github.com/mateusz/carryall/engine/entities"
	"github.com/mateusz/carryall/engine/sid"
	"github.com/mateusz/carryall/piksele"
	"gitlab.com/gomidi/midi"
	"gitlab.com/gomidi/midi/midimessage/channel"
	"gitlab.com/gomidi/midi/writer"
)

const SID_CHAN_ENGINE = "engine"
const SID_CHAN_ENGINE_WHOOSH = "engineWhoosh"
const SID_CHAN_CREAKING = "creaking"
const SID_CHAN_GROUND_ALERT = "groundAlert"
const SID_CHAN_STRESS_ALERT = "stressAlert"
const SID_CHAN_EXPLOSION = "explosion"

type Carryall struct {
	// Various physics settings
	bodyRotationLimit        float64
	bodyRotationSpeed        float64
	stabilityPower           float64
	currentStabilityPower    float64
	engineRotationLimit      float64
	engineRotationSpeed      float64
	enginePower              float64
	engineRedirectMultiplier float64
	currentEnginePower       float64
	drag                     float64
	bounceDampen             pixel.Vec

	// Audio
	engineSound *sid.Vibrato

	// State
	engineSpinup        float64
	position            pixel.Vec
	bodyRotation        float64
	engineRotation      float64
	velocity            pixel.Vec
	currentDrag         pixel.Vec
	avgVelocity         *movingAverage
	destroyingStart     time.Time
	destroyingAudioDone bool
	accelerationStress  float64
	engineSpinupStart   time.Time
	engineSpinupDone    bool
	stressLightOn       bool
	atmoPressure        float64

	// Input counters
	leftPanTicks  int64
	leftBalVal    float64 // Absolute value [0.0,1.0]
	rightPanTicks int64
	rightBalVal   float64 // Absolute value [0.0,1.0]
	middleBalVal  float64 // Absolute value [0.0,1.0]
	playIsHeld    bool

	// Sprites
	body         *piksele.Sprite
	stabilityJet *piksele.Sprite
	engine       *piksele.Sprite
	engineJet    *piksele.Sprite
	explosion    *piksele.Sprite
}

func NewCarryall(mobSprites, mobSprites32 *piksele.Spriteset) Carryall {
	c := Carryall{
		bodyRotationLimit:   math.Pi / 1.5,
		bodyRotationSpeed:   1.0 / 4.0,
		stabilityPower:      1.0,
		engineRotationLimit: math.Pi / 1.5,
		engineRotationSpeed: 1.0 / 4.0,
		enginePower:         4.0,
		drag:                0.005,
		bounceDampen:        pixel.Vec{X: 0.75, Y: -0.5},

		velocity:     pixel.Vec{X: 0.0, Y: gravity.Y},
		middleBalVal: 0.5,
		leftBalVal:   0.0,
		rightBalVal:  0.5,
		// Starts vertical, but needs to be horizontal
		engineRotation: -3.14 / 2.0,
		avgVelocity:    newMovingAverage(pixel.Vec{}, 1000, time.Millisecond),

		body: &piksele.Sprite{
			Spriteset: mobSprites32,
			SpriteID:  SPR_32_CARRYALL,
		},
		engine: &piksele.Sprite{
			Spriteset: mobSprites,
			SpriteID:  SPR_16_ENGINE,
		},
		stabilityJet: &piksele.Sprite{
			Spriteset: mobSprites32,
			SpriteID:  SPR_32_STABILITY_JET,
		},
		engineJet: &piksele.Sprite{
			Spriteset: mobSprites32,
			SpriteID:  SPR_32_ENGINE_JET,
		},
		explosion: &piksele.Sprite{
			Spriteset: mobSprites,
			SpriteID:  SPR_16_EXPLOSION_START,
		},
	}

	return c
}

func (s Carryall) Draw(onto pixel.Target) {
	if s.destroyingStart.After(startTime) {
		time.Since(s.destroyingStart)
		frame := uint32(time.Since(s.destroyingStart) / (50 * time.Millisecond))
		if SPR_16_EXPLOSION_START+frame > SPR_16_EXPLOSION_END {
			return
		}

		bodyTransform := pixel.IM.Scaled(pixel.Vec{X: 0.0, Y: 0.0}, 10.0).Rotated(pixel.Vec{X: 0.0, Y: 0.0}, s.bodyRotation).Moved(s.position)
		s.explosion.Spriteset.Sprites[s.explosion.SpriteID+frame].Draw(
			onto,
			bodyTransform,
		)
		return
	}

	bodyTransform := pixel.IM.Rotated(pixel.Vec{X: 0.0, Y: 0.0}, s.bodyRotation).Moved(s.position)
	s.body.Spriteset.Sprites[s.body.SpriteID].Draw(
		onto,
		bodyTransform,
	)
	frame := uint32(time.Now().Sub(startTime) / (50 * time.Millisecond) % 2)
	scaleStab := s.currentStabilityPower / s.stabilityPower
	s.stabilityJet.Spriteset.Sprites[s.stabilityJet.SpriteID+frame].DrawColorMask(
		onto,
		pixel.IM.ScaledXY(pixel.Vec{X: 0.0, Y: -3.0}, pixel.Vec{X: 1.0, Y: scaleStab}).Moved(pixel.Vec{X: 0.0, Y: -5.0}).Chained(bodyTransform),
		color.Alpha{A: 127},
	)

	engineTransform := pixel.IM.Rotated(pixel.Vec{X: 0.0, Y: 0.0}, s.engineRotation).Moved(pixel.Vec{X: 0.0, Y: 3.0}).Chained(bodyTransform)
	s.engine.Spriteset.Sprites[s.engine.SpriteID].Draw(
		onto,
		engineTransform,
	)
	if s.rightBalVal > 0.55 {
		scale := s.currentEnginePower / s.enginePower
		s.engineJet.Spriteset.Sprites[s.engineJet.SpriteID+frame].DrawColorMask(
			onto,
			pixel.IM.ScaledXY(pixel.Vec{X: -1.0, Y: 16.0}, pixel.Vec{X: scale, Y: scale}).Moved(pixel.Vec{X: 1.0, Y: -23.0}).Chained(engineTransform),
			color.Alpha{A: 127},
		)
	}
	if s.rightBalVal < 0.45 {
		scale := s.currentEnginePower / s.enginePower
		s.engineJet.Spriteset.Sprites[s.engineJet.SpriteID+frame].DrawColorMask(
			onto,
			pixel.IM.ScaledXY(pixel.Vec{X: -1.0, Y: 16.0}, pixel.Vec{X: scale, Y: scale}).Moved(pixel.Vec{X: 1.0, Y: -8.0}).Chained(engineTransform),
			color.Alpha{A: 127},
		)
	}
}

func (s Carryall) GetZ() float64 {
	return -s.position.Y
}

func (s Carryall) GetX() float64 {
	return s.position.X
}

func (s Carryall) GetY() float64 {
	return s.position.Y
}

func (s *Carryall) Step(dt float64) {
	if s.destroyingStart.After(startTime) {
		return
	}

	factor := 0.02

	if s.playIsHeld && s.engineSpinup < 1.0 {
		if s.engineSpinupStart.Before(startTime) {
			s.engineSpinupStart = time.Now()
		}
		s.engineSpinup += 0.25 * factor
		if s.engineSpinup > 1.0 {
			s.engineSpinup = 1.0
		}
	}
	if !s.playIsHeld && s.engineSpinup > 0.0 && s.engineSpinup < 1.0 {
		s.engineSpinup -= 0.15 * factor
	}

	s.bodyRotation += -factor * 3.14 * float64(s.leftPanTicks) * s.bodyRotationSpeed
	if s.bodyRotation < -s.bodyRotationLimit {
		s.bodyRotation = -s.bodyRotationLimit
	}
	if s.bodyRotation > s.bodyRotationLimit {
		s.bodyRotation = s.bodyRotationLimit
	}

	// Jets are drawn vertical, but positioned horizontal
	s.engineRotation += -factor * 3.14 * float64(s.rightPanTicks) * s.engineRotationSpeed
	jetRangeMin := -math.Pi/2.0 - s.engineRotationLimit
	jetRangeMax := -math.Pi/2.0 + s.engineRotationLimit
	if s.engineRotation < jetRangeMin {
		s.engineRotation = jetRangeMin
	}
	if s.engineRotation > jetRangeMax {
		s.engineRotation = jetRangeMax
	}

	s.atmoPressure = 1.0 - (s.position.Y-1000.0)/2000.0
	if s.atmoPressure > 1.0 {
		s.atmoPressure = 1.0
	}
	if s.atmoPressure < 0.0 {
		s.atmoPressure = 0.0
	}

	s.currentStabilityPower = s.leftBalVal * (s.stabilityPower * (1.0 - s.middleBalVal)) * s.engineSpinup * s.atmoPressure
	s.currentEnginePower = (s.rightBalVal - 0.5) * 2.0 * (s.enginePower * s.middleBalVal) * s.engineSpinup * s.atmoPressure

	// [0.0, 1.0], this is just for hovering, can't reverse
	dvBody := pixel.Vec{X: 0, Y: s.currentStabilityPower}.Rotated(s.bodyRotation)
	// Shifted to [-0.5, 0.5], jets can go backwards. Also increase power - main jet is super-powerful.
	dvJet := pixel.Vec{X: 0, Y: s.currentEnginePower}.Rotated(s.bodyRotation).Rotated(s.engineRotation)

	s.currentDrag = s.velocity.Scaled(-s.drag).Scaled(s.atmoPressure)

	velocityBefore := s.velocity

	s.velocity = s.velocity.Add(s.currentDrag)
	s.velocity = s.velocity.Add(dvBody)
	s.velocity = s.velocity.Add(dvJet)
	s.velocity = s.velocity.Add(gravity)
	s.position = s.position.Add(s.velocity.Scaled(factor))

	s.accelerationStress = velocityBefore.Sub(s.velocity).Len()

	if s.position.Y < 167.0 {
		if math.Abs(s.bodyRotation) > (math.Pi / 8.0) {
			s.destroyingStart = time.Now()
		} else {
			s.velocity = s.velocity.ScaledXY(s.bounceDampen)
			s.position.Y = 167.0
			s.bodyRotation = 0.0
			// Leg springs dampen the impact ;-)
			s.accelerationStress = velocityBefore.Sub(s.velocity).Len() / 15.0
		}
	}

	if time.Since(startTime) > 3.0*time.Second && s.accelerationStress > 3.8 {
		// crash
		s.destroyingStart = time.Now()
	}

	if s.velocity.Len() < 100.0 {
		s.avgVelocity.sample(s.velocity)
	} else {
		s.avgVelocity.sample(s.velocity.Unit().Scaled(100.0))
	}
}

func (s *Carryall) Input(win *pixelgl.Window, ref pixel.Matrix) {
	if s.destroyingStart.After(startTime) {
		return
	}

	if win.Pressed(pixelgl.KeyLeft) {
		s.velocity = s.velocity.Add(pixel.Vec{X: -10.0})
	}
	if win.Pressed(pixelgl.KeyRight) {
		s.velocity = s.velocity.Add(pixel.Vec{X: 10.0})
	}
	if win.Pressed(pixelgl.KeyUp) {
		s.velocity = s.velocity.Add(pixel.Vec{Y: 10.0})
	}
	if win.Pressed(pixelgl.KeyDown) {
		s.velocity = s.velocity.Add(pixel.Vec{Y: -10.0})
	}
}

func (s *Carryall) MidiInput(msgs []midi.Message) {
	if s.destroyingStart.After(startTime) {
		return
	}

	s.leftPanTicks = 0
	s.rightPanTicks = 0
	for _, m := range msgs {
		non, ok := m.(channel.NoteOn)
		if ok {
			if non.Channel() == engine.MIDI_CHAN_LEFT && non.Key() == engine.MIDI_KEY_PLAY {
				s.playIsHeld = true
			}
			if non.Channel() == engine.MIDI_CHAN_LEFT && non.Key() == engine.MIDI_KEY_SYNC {
				s.engineSpinupStart = time.Now()
				s.engineSpinupDone = false
				s.engineSpinup = 0.9
			}
		}
		noff, ok := m.(channel.NoteOff)
		if ok {
			if noff.Channel() == engine.MIDI_CHAN_LEFT && noff.Key() == engine.MIDI_KEY_PLAY {
				s.playIsHeld = false
			}
		}

		cc, ok := m.(channel.ControlChange)
		if ok {
			if cc.Channel() == engine.MIDI_CHAN_LEFT && (cc.Controller() == engine.MIDI_CTRL_RIM || cc.Controller() == engine.MIDI_CTRL_PAN) {
				if cc.Value() == engine.MIDI_VAL_PAN_CCW {
					s.leftPanTicks--
				} else if cc.Value() == engine.MIDI_VAL_PAN_CW {
					s.leftPanTicks++
				}
			}

			if cc.Channel() == engine.MIDI_CHAN_LEFT && cc.Controller() == engine.MIDI_CTRL_BALANCE_MSB {
				// Scaled to 0.0-1.0
				s.leftBalVal = float64(cc.Value()) / 127
			}

			if cc.Channel() == engine.MIDI_CHAN_RIGHT && (cc.Controller() == engine.MIDI_CTRL_RIM || cc.Controller() == engine.MIDI_CTRL_PAN) {
				if cc.Value() == engine.MIDI_VAL_PAN_CCW {
					s.rightPanTicks--
				} else if cc.Value() == engine.MIDI_VAL_PAN_CW {
					s.rightPanTicks++
				}
			}

			if cc.Channel() == engine.MIDI_CHAN_RIGHT && cc.Controller() == engine.MIDI_CTRL_BALANCE_MSB {
				// Scaled to 0.0-1.0
				s.rightBalVal = float64(cc.Value()) / 127
			}

			if cc.Channel() == engine.MIDI_CHAN_MIDDLE && cc.Controller() == engine.MIDI_CTRL_BANK_SELECT_MSB {
				// Scaled to 0.0-1.0
				s.middleBalVal = float64(cc.Value()) / 127
			}

		}
	}
}

func (s *Carryall) MidiOutput(wr *writer.Writer) {
	wr.SetChannel(engine.MIDI_CHAN_RIGHT)
	if s.accelerationStress >= 2.8 && !s.stressLightOn {
		s.stressLightOn = true
		writer.NoteOn(wr, engine.MIDI_KEY_SYNC, 0x7F)
	}
	if s.accelerationStress < 2.8 && s.stressLightOn {
		s.stressLightOn = false
		writer.NoteOff(wr, engine.MIDI_KEY_SYNC)
	}

	wr.SetChannel(engine.MIDI_CHAN_LEFT)
	if !s.engineSpinupDone && s.engineSpinupStart.After(startTime) {
		spinupFrame := uint32(time.Since(s.engineSpinupStart) / (time.Millisecond * 250.0))
		if s.engineSpinup == 1.0 {
			s.engineSpinupStart = time.Time{}
			s.engineSpinupDone = true
			wr.SetChannel(engine.MIDI_CHAN_LEFT)
			writer.NoteOn(wr, engine.MIDI_KEY_PLAY, 0x7F)
		} else if !s.playIsHeld {
			s.engineSpinupStart = time.Time{}
			wr.SetChannel(engine.MIDI_CHAN_LEFT)
			writer.NoteOff(wr, engine.MIDI_KEY_PLAY)
		} else if spinupFrame%2 == 1 {
			wr.SetChannel(engine.MIDI_CHAN_LEFT)
			writer.NoteOn(wr, engine.MIDI_KEY_PLAY, 0x7F)
		} else {
			wr.SetChannel(engine.MIDI_CHAN_LEFT)
			writer.NoteOff(wr, engine.MIDI_KEY_PLAY)
		}
	}

}

func (s *Carryall) GetChannels() map[string]*sid.Channel {
	return map[string]*sid.Channel{
		SID_CHAN_ENGINE:        sid.NewChannel(0.0),
		SID_CHAN_ENGINE_WHOOSH: sid.NewChannel(0.0),
		SID_CHAN_CREAKING:      sid.NewChannel(0.0),
		SID_CHAN_GROUND_ALERT:  sid.NewChannel(0.2),
		SID_CHAN_STRESS_ALERT:  sid.NewChannel(0.2),
		SID_CHAN_EXPLOSION:     sid.NewChannel(0.25),
	}
}

func (s *Carryall) SetupChannels(onto *sid.Sid) {
	s.engineSound = sid.NewVibrato(20.0, 1.02, 1.05)
	onto.SetSource(SID_CHAN_ENGINE, s.engineSound)

	onto.SetSource(SID_CHAN_GROUND_ALERT, sid.NewMp3("assets/ground_alert.mp3", true))
	onto.Pause(SID_CHAN_GROUND_ALERT)

	onto.SetSource(SID_CHAN_STRESS_ALERT, sid.NewMp3("assets/stress_alert3.mp3", true))
	onto.Pause(SID_CHAN_STRESS_ALERT)

	onto.SetSource(SID_CHAN_ENGINE_WHOOSH, sid.NewPinkNoise(5))

	onto.SetSource(SID_CHAN_CREAKING, sid.NewMp3("assets/submarine_breaking3.mp3", true))

	onto.SetSource(SID_CHAN_EXPLOSION, sid.NewMp3("assets/explosion2.mp3", false))
	onto.Pause(SID_CHAN_EXPLOSION)
}

func (s *Carryall) MakeNoise(onto *sid.Sid) {
	if s.destroyingAudioDone {
		return
	}
	if s.destroyingStart.After(startTime) {
		onto.Pause(SID_CHAN_STRESS_ALERT)
		onto.Pause(SID_CHAN_GROUND_ALERT)
		onto.Pause(SID_CHAN_CREAKING)
		onto.Pause(SID_CHAN_ENGINE)
		onto.Pause(SID_CHAN_ENGINE_WHOOSH)

		onto.Resume(SID_CHAN_EXPLOSION)

		s.destroyingAudioDone = true
		return
	}
	if s.engineSpinup < 0.05 {
		onto.SetVolume(SID_CHAN_ENGINE, 0.0)
	}

	if s.accelerationStress > 2.8 {
		onto.Resume(SID_CHAN_STRESS_ALERT)
	} else {
		onto.Pause(SID_CHAN_STRESS_ALERT)
	}

	if s.position.Add(s.velocity.Scaled(3.0)).Y < 167.0 && s.velocity.Len() > 30.0 {
		if onto.IsPaused(SID_CHAN_GROUND_ALERT) {
			//onto.Reset(SID_CHAN_GROUND_ALERT)
			onto.Resume(SID_CHAN_GROUND_ALERT)
		}
	} else {
		onto.Pause(SID_CHAN_GROUND_ALERT)
	}

	if s.accelerationStress > 1.5 {
		onto.Resume(SID_CHAN_CREAKING)
		stressLevel := (s.accelerationStress - 1.5) / 2.3
		if stressLevel > 1.0 {
			stressLevel = 1.0
		}
		onto.SetVolume(SID_CHAN_CREAKING, math.Sqrt(stressLevel)*0.25+0.05)
	} else {
		onto.Pause(SID_CHAN_CREAKING)
	}

	// Map to [0.0 - 1.0]
	whooshVol := s.currentDrag.Len()
	if whooshVol > 1.0 {
		whooshVol = 1.0
	}
	onto.SetVolume(SID_CHAN_ENGINE_WHOOSH, 0.02*math.Pow(whooshVol, 1.8))

	// Map controls to [0.0 - 1.0]
	maxPower := p1.carryall.stabilityPower + p1.carryall.enginePower
	totalThrottle := (p1.carryall.currentStabilityPower + math.Abs(p1.carryall.currentEnginePower)) / maxPower
	s.engineSound.SetFreq(math.Sqrt(p1.carryall.velocity.Len()) + s.engineSpinup*10.0 + 5.0)

	if s.playIsHeld {
		v := totalThrottle*0.75 + 1.0
		if v > 1.0 {
			v = 1.0
		}
		onto.SetVolume(SID_CHAN_ENGINE, v*0.2)
	} else if s.engineSpinup > 0.03 {
		onto.SetVolume(SID_CHAN_ENGINE, (totalThrottle*0.75+0.25)*0.2)
	}
}
