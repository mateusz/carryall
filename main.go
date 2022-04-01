package main

import (
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"github.com/faiface/beep/speaker"
	"github.com/faiface/pixel"
	"github.com/faiface/pixel/pixelgl"
	engine "github.com/mateusz/carryall/engine/entities"
	"github.com/mateusz/carryall/engine/sid"
	"github.com/mateusz/carryall/piksele"
	"golang.org/x/image/colornames"
)

var (
	workDir        string
	monW           float64
	monH           float64
	zoom           float64
	mobSprites     piksele.Spriteset
	mobSprites32   piksele.Spriteset
	audioSamples   map[int32]audioSample
	cursorSprites  piksele.Spriteset
	p1             player
	gameWorld      piksele.World
	gameEntities   engine.Entities
	mc             midiController
	startTime      time.Time
	mainBackground background
	mainStarfield  starfield
	freq           float64
	audio          *sid.Sid
	engineSound    *sid.Vibrato
	whoosh         *sid.PinkNoise
)

func main() {
	startTime = time.Now()
	rand.Seed(time.Now().UnixNano())

	var err error
	workDir, err = filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		fmt.Printf("Error checking working dir: %s\n", err)
		os.Exit(2)
	}

	gameWorld = piksele.World{}
	gameWorld.Load(fmt.Sprintf("%s/assets/level3.tmx", workDir))

	mobSprites, err = piksele.NewSpritesetFromTsx(fmt.Sprintf("%s/assets", workDir), "sprites.tsx")
	if err != nil {
		fmt.Printf("Error loading sprites.tsx: %s\n", err)
		os.Exit(2)
	}

	mobSprites32, err = piksele.NewSpritesetFromTsx(fmt.Sprintf("%s/assets", workDir), "sprites32.tsx")
	if err != nil {
		fmt.Printf("Error loading sprites32.tsx: %s\n", err)
		os.Exit(2)
	}

	gameEntities = engine.NewEntities()
	carryall := NewCarryall(&mobSprites, &mobSprites32)
	carryall.position = pixel.Vec{X: 768.0, Y: 168.0}
	carryall.velocity = pixel.Vec{X: 0.0, Y: 0.5}

	gameEntities = gameEntities.Add(&carryall)

	p1.position = pixel.Vec{X: 256.0, Y: 256.0}
	p1.carryall = &carryall

	mc, err = newMidiController()
	defer mc.close()

	mainBackground = newBackogrund(16.0, []stripe{
		{pos: -20, colour: makeColourful(colornames.Black)},
		{pos: 0, colour: makeColourful(colornames.Saddlebrown)},
		{pos: 9, colour: makeColourful(color.RGBA{R: 217, G: 160, B: 102, A: 255})},
		{pos: 10, colour: makeColourful(colornames.Green)},
		{pos: 30, colour: makeColourful(colornames.Pink)},
		{pos: 70, colour: makeColourful(color.RGBA{R: 170, G: 52, B: 195, A: 255})},
		{pos: 110, colour: makeColourful(color.RGBA{R: 7, G: 15, B: 78, A: 255})},
		{pos: 200, colour: makeColourful(colornames.Black)},
	})

	chmap := make(map[string]*sid.Channel)
	for chName, ch := range p1.carryall.GetChannels() {
		chmap[chName] = ch
	}

	audio = sid.New(chmap)
	carryall.SetupChannels(audio)
	audio.Start(44100.0)

	audioSamples = make(map[int32]audioSample)
	audioSamples[MP3_EXPLOSION] = newSampleMp3("explosion")
	speaker.Init(44100, 8)

	pixelgl.Run(run)
}

func run() {
	monitor := pixelgl.PrimaryMonitor()
	monW, monH = monitor.Size()

	mainStarfield = newStarfield(monW, monH)

	cfg := pixelgl.WindowConfig{
		Title:   "Carryall",
		Bounds:  pixel.R(0, 0, monW, monH),
		VSync:   true,
		Monitor: monitor,
	}

	win, err := pixelgl.NewWindow(cfg)
	if err != nil {
		panic(err)
	}

	// Get nice pixels
	win.SetSmooth(false)
	win.SetMousePosition(pixel.Vec{X: monW / 2.0, Y: 0.0})

	mapCanvas := pixelgl.NewCanvas(pixel.R(0, 0, float64(gameWorld.PixelWidth()), float64(gameWorld.PixelHeight())))
	gameWorld.Draw(mapCanvas)

	last := time.Now()
	var avgVelocity pixel.Vec
	var percMax, zoom float64
	var dt float64
	var cam1 pixel.Matrix
	var worldOffset pixel.Matrix
	p1view := pixelgl.NewCanvas(pixel.R(0, 0, monW, monH))
	p1bg := pixelgl.NewCanvas(pixel.R(0, 0, monW, monH))
	p1bgOverlay := pixelgl.NewCanvas(pixel.R(0, 0, monW, monH))

	prof, _ := os.Create("cpuprof.prof")
	defer prof.Close()
	defer pprof.StopCPUProfile()
	pprof.StartCPUProfile(prof)

	for !win.Closed() {
		// Exit conditions
		if win.JustPressed(pixelgl.KeyEscape) {
			break
		}

		// Update world state
		dt = time.Since(last).Seconds()
		last = time.Now()

		p1.Input(win, cam1)
		p1.Update(dt)

		if p1.carryall.position.X < 0.0 {
			p1.carryall.position.X += mapCanvas.Bounds().W()
		}
		if p1.carryall.position.X >= mapCanvas.Bounds().W() {
			p1.carryall.position.X -= mapCanvas.Bounds().W()
		}

		gameEntities.Input(win, cam1)
		gameEntities.MidiInput(mc.queue)
		gameEntities.Step(dt)

		// Sound
		p1.carryall.MakeNoise(audio)

		// Paint
		win.Clear(colornames.Navy)
		p1view.Clear(color.RGBA{A: 0.0})
		p1bg.Clear(colornames.Lightgreen)
		p1bgOverlay.Clear(color.RGBA{A: 0.0})

		avgVelocity = p1.carryall.avgVelocity.average()
		percMax = (avgVelocity.Len() / 200.0)
		zoom = 4.0 / (math.Pow(2.0, 2.0*percMax))
		p1.position = p1.carryall.position.Add(p1.carryall.avgVelocity.average())

		worldOffset = pixel.IM
		worldOffset = worldOffset.Moved(p1.position.Scaled(-1.0))
		worldOffset = worldOffset.Scaled(pixel.Vec{}, zoom)
		worldOffset = worldOffset.Moved(pixel.Vec{X: monW / 2.0, Y: monH / 2.0})
		p1view.SetMatrix(worldOffset)
		p1bg.SetMatrix(worldOffset)

		mainBackground.Draw(p1bg, -mapCanvas.Bounds().W(), mapCanvas.Bounds().W()*2)

		starAlpha := ((p1.position.Y - 1600.0) / 1000.0)
		if starAlpha > 1.0 {
			starAlpha = 1.0
		}
		if starAlpha > 0 {
			p1bgOverlay.SetColorMask(pixel.Alpha(starAlpha))
			mainStarfield.Draw(p1bgOverlay)
		}

		mapCanvas.Draw(p1view, pixel.IM.Moved(pixel.Vec{
			X: mapCanvas.Bounds().W() / 2.0,
			Y: mapCanvas.Bounds().H() / 2.0,
		}))
		mapCanvas.Draw(p1view, pixel.IM.Moved(pixel.Vec{
			X: mapCanvas.Bounds().W()/2.0 + mapCanvas.Bounds().W(),
			Y: mapCanvas.Bounds().H() / 2.0,
		}))
		mapCanvas.Draw(p1view, pixel.IM.Moved(pixel.Vec{
			X: mapCanvas.Bounds().W()/2.0 - mapCanvas.Bounds().W(),
			Y: mapCanvas.Bounds().H() / 2.0,
		}))

		gameEntities.ByZ().Draw(p1view)

		p1bg.Draw(win, pixel.IM.Moved(pixel.Vec{
			X: p1bg.Bounds().W() / 2,
			Y: p1bg.Bounds().H() / 2,
		}))
		p1bgOverlay.Draw(win, pixel.IM.Moved(pixel.Vec{X: monW / 2.0, Y: monH / 2.0}))
		p1view.Draw(win, pixel.IM.Moved(pixel.Vec{
			X: p1view.Bounds().W() / 2,
			Y: p1view.Bounds().H() / 2,
		}))
		win.Update()
	}

	audio.Close()

	for _, v := range audioSamples {
		v.streamer.Close()
	}
}
