package main

import (
	_ "embed"

	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gonutz/w32/v2"
	"github.com/gonutz/wui/v2"
)

//go:embed red_icon.png
var redIconPNG []byte

//go:embed green_icon.png
var greenIconPNG []byte

const (
	appName         = "work_timer"
	WM_TOGGLE_STATE = w32.WM_USER + 1
)

var (
	running      = false
	pauseStart   time.Time
	lastStart    time.Time
	lastSeconds  = 0
	todaySeconds = 0
	window       *wui.Window
	canvas       *wui.PaintBox
	workTimes    [][2]time.Time
	redIcon      *wui.Icon
	greenIcon    *wui.Icon
)

func main() {
	existingWindow := w32.FindWindow(appName, "")
	if existingWindow != 0 {
		w32.SendMessage(existingWindow, WM_TOGGLE_STATE, 0, 0)
		return
	}

	readLog()

	redIconImage, _ := png.Decode(bytes.NewReader(redIconPNG))
	redIcon, _ = wui.NewIconFromImage(redIconImage)
	greenIconImage, _ := png.Decode(bytes.NewReader(greenIconPNG))
	greenIcon, _ = wui.NewIconFromImage(greenIconImage)

	window = wui.NewWindow()
	window.SetClassName(appName)
	font, _ := wui.NewFont(wui.FontDesc{Name: "Tahoma", Height: -19})
	window.SetFont(font)
	window.SetTitle("Pause")
	window.SetIcon(redIcon)
	window.SetPosition(0, 8)
	window.SetHasMinButton(false)
	window.SetHasMaxButton(false)
	window.SetInnerSize(250, 300)

	canvas = wui.NewPaintBox()
	canvas.SetSize(window.InnerSize())
	window.Add(canvas)
	canvas.SetOnPaint(drawState)

	window.SetOnMessage(func(
		window uintptr, msg uint32, w, l uintptr,
	) (handled bool, result uintptr) {
		switch msg {
		case WM_TOGGLE_STATE:
			toggleState()
			handled = true
		case w32.WM_POWERBROADCAST:
			if w == w32.PBT_APMSUSPEND {
				stop()
			}
			handled = true
			result = 1
		}
		return
	})

	window.SetOnCanClose(func() bool {
		if running {
			return wui.MessageBoxCustom(
				"Done yet?",
				"You're still working, really close this program?",
				w32.MB_YESNO|w32.MB_ICONQUESTION|w32.MB_DEFBUTTON2,
			) == w32.IDYES
		}
		return true
	})

	window.SetShortcut(toggleState, wui.KeySpace)

	go func() {
		for {
			if running {
				lastSeconds = int(time.Now().Sub(lastStart).Seconds() + 0.5)
			}
			canvas.Paint()
			time.Sleep(time.Second)
		}
	}()

	window.Show()

	stop()
}

func drawState(c *wui.Canvas) {
	back := wui.RGB(255, 192, 192)
	if running {
		back = wui.RGB(192, 255, 192)
	}
	c.FillRect(0, 0, c.Width(), c.Height(), back)

	caption := "Press SPACE to "
	if running {
		caption += "stop."
	} else {
		caption += "start."
	}
	w, _ := c.TextExtent(caption)
	c.TextOut((c.Width()-w)/2, 5, caption, wui.RGB(66, 66, 66))

	text := "Worked " + secondsToString(todaySeconds+lastSeconds) + " today"
	w, h := c.TextExtent(text)
	y := (c.Height() - h) / 2
	c.TextOut((c.Width()-w)/2, y, text, wui.RGB(0, 0, 0))

	var empty time.Time
	if !running && pauseStart != empty {
		pauseSeconds := int(time.Now().Sub(pauseStart).Seconds() + 0.5)
		text = "Pausing for " + secondsToString(pauseSeconds)
		w, h = c.TextExtent(text)
		y += h + h/2
		c.TextOut((c.Width()-w)/2, y, text, wui.RGB(0, 0, 0))
	}
}

func secondsToString(sec int) string {
	minutes := (sec + 30) / 60
	return fmt.Sprintf("%d:%02d", minutes/60, minutes%60)
}

func toggleState() {
	if running {
		stop()
	} else {
		start()
	}
}

func start() {
	if running {
		return
	}
	running = true
	lastStart = time.Now()
	window.SetTitle("Working")
	window.SetIcon(greenIcon)
	computeTodaySeconds()
	canvas.Paint()
}

func stop() {
	if !running {
		return
	}
	running = false
	end := time.Now()
	pauseStart = end
	todaySeconds += lastSeconds
	lastSeconds = 0
	workTimes = append(workTimes, [2]time.Time{lastStart, end})
	updateLog()
	window.SetIcon(redIcon)
	window.SetTitle("Pause")
	canvas.Paint()
}

func logPath() string {
	return filepath.Join(os.Getenv("APPDATA"), "work_times.csv")
}

func readLog() {
	// If file does not yet exist, it is fine.
	_, err := os.Stat(logPath())
	if err != nil {
		return
	}

	data, err := os.ReadFile(logPath())
	check(err)
	lines := strings.Split(strings.Replace(string(data), "\r", "", -1), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 2 {
			fail("each line must have 2 parts separated by a comma: " + line)
		}
		var start, end time.Time
		check(start.UnmarshalText([]byte(parts[0])))
		check(end.UnmarshalText([]byte(parts[1])))
		workTimes = append(workTimes, [2]time.Time{start, end})
	}

	sort.Slice(workTimes, func(i, j int) bool {
		return workTimes[i][0].Before(workTimes[j][1])
	})

	computeTodaySeconds()
}

func computeTodaySeconds() {
	now := time.Now()
	todaySeconds = 0
	for _, span := range workTimes {
		start, end := span[0], span[1]
		if sameDay(now, start) {
			todaySeconds += int(end.Sub(start).Seconds() + 0.5)
		}
	}
}

func sameDay(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func updateLog() {
	f, err := os.Create(logPath())
	check(err)
	defer f.Close()

	for _, span := range workTimes {
		start := span[0]
		startText, err := start.MarshalText()
		check(err)

		end := span[1]
		endText, err := end.MarshalText()
		check(err)

		f.Write(startText)
		f.Write([]byte{','})
		f.Write(endText)
		f.Write([]byte{'\n'})
	}
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	wui.MessageBoxError("Error", msg)
	os.Exit(1)
}
