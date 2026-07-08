package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getlantern/systray"
)

type TrayManager struct {
	app     *App
	once    sync.Once
	stopper sync.Once
	ready   atomic.Bool
}

func NewTrayManager(app *App) *TrayManager {
	return &TrayManager{app: app}
}

func (t *TrayManager) Start() {
	if runtime.GOOS != "windows" {
		return
	}
	go t.once.Do(func() {
		runtime.LockOSThread()
		t.app.appendLog("tray starting")
		systray.Run(t.onReady, t.onExit)
	})
}

func (t *TrayManager) Stop() {
	if runtime.GOOS != "windows" {
		return
	}
	t.stopper.Do(func() {
		systray.Quit()
	})
}

func (t *TrayManager) Ready() bool {
	return t != nil && t.ready.Load()
}

func (t *TrayManager) onReady() {
	t.ready.Store(true)
	t.app.appendLog("tray ready")
	systray.SetIcon(hindsightTrayIcon())
	systray.SetTitle("Hindsight")
	systray.SetTooltip("Hindsight Local Manager - right-click for menu")

	show := systray.AddMenuItem("Show", "Show Hindsight Local Manager")
	start := systray.AddMenuItem("Start Services", "Start Hindsight API and UI")
	stop := systray.AddMenuItem("Stop Services", "Stop managed services")
	systray.AddSeparator()
	openUI := systray.AddMenuItem("Open Hindsight UI", "Open Hindsight UI")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Quit Hindsight Local Manager")

	go func() {
		lastShow := time.Time{}
		for {
			select {
			case <-show.ClickedCh:
				if time.Since(lastShow) < 250*time.Millisecond {
					continue
				}
				lastShow = time.Now()
				t.app.appendLog("tray show clicked")
				t.app.ShowWindow()
			case <-start.ClickedCh:
				t.app.appendLog("tray start clicked")
				go func() {
					if err := t.app.StartAll(); err != nil {
						t.app.appendLog("tray start failed: " + err.Error())
					}
				}()
			case <-stop.ClickedCh:
				t.app.appendLog("tray stop clicked")
				go func() {
					if err := t.app.StopAll(); err != nil {
						t.app.appendLog("tray stop failed: " + err.Error())
					}
				}()
			case <-openUI.ClickedCh:
				t.app.appendLog("tray open ui clicked")
				go func() {
					if err := t.app.OpenControlPlane(); err != nil {
						t.app.appendLog("tray open ui failed: " + err.Error())
					}
				}()
			case <-quit.ClickedCh:
				t.app.appendLog("tray quit clicked")
				t.app.QuitApp()
				return
			}
		}
	}()
}

func (t *TrayManager) onExit() { t.ready.Store(false) }

func hindsightTrayIcon() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			dx, dy := x-15, y-15
			if dx*dx+dy*dy > 15*15 {
				continue
			}
			img.SetRGBA(x, y, color.RGBA{R: uint8(92 + x*3), G: uint8(45 + y*2), B: 210, A: 255})
		}
	}
	for y := 8; y < 25; y++ {
		for x := 9; x < 13; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 238, G: 247, B: 243, A: 255})
		}
		for x := 20; x < 24; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 238, G: 247, B: 243, A: 255})
		}
	}
	for y := 15; y < 19; y++ {
		for x := 9; x < 24; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 115, G: 251, B: 211, A: 255})
		}
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return nil
	}
	pngBytes := pngBuf.Bytes()
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, uint16(0))
	_ = binary.Write(&ico, binary.LittleEndian, uint16(1))
	_ = binary.Write(&ico, binary.LittleEndian, uint16(1))
	ico.WriteByte(32)
	ico.WriteByte(32)
	ico.WriteByte(0)
	ico.WriteByte(0)
	_ = binary.Write(&ico, binary.LittleEndian, uint16(1))
	_ = binary.Write(&ico, binary.LittleEndian, uint16(32))
	_ = binary.Write(&ico, binary.LittleEndian, uint32(len(pngBytes)))
	_ = binary.Write(&ico, binary.LittleEndian, uint32(22))
	ico.Write(pngBytes)
	return ico.Bytes()
}
