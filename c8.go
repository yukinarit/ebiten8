package main

import (
	"fmt"
	"image/color"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

const (
	V_PIXELS     = 32
	H_PIXELS     = 64
	SCALE        = 10
	WIDTH        = H_PIXELS * SCALE
	HEIGHT       = V_PIXELS * SCALE
	BUTTON_WIDTH = 80 // Button width of Game Select UI
	BUTTON_HIGHT = 23 // Button height of Game Select UI
	SELECT_HIGHT = 45 // Title height of Game Select UI
)

// A pixel in Chip8 console.
type Pixel struct {
	x      int
	y      int
	enable bool
}

func (p *Pixel) image() *ebiten.Image {
	img := ebiten.NewImage(10, 10)
	if p.enable {
		img.Fill(color.White)
	} else {
		img.Fill(color.Black)
	}
	return img
}

func (p *Pixel) Draw(screen *ebiten.Image) {
	opts := &ebiten.DrawImageOptions{}
	opts.GeoM.Translate(float64(10*p.x), float64(10*p.y))
	screen.DrawImage(p.image(), opts)
}

// Game main.
type Chip8 struct {
	cpu   *Cpu
	mem   *Memory
	vme   *VideoMemory
	audio *audio.Player
	kb    *Keyboard
}

func (c8 *Chip8) Update() {
	c8.kb.Update()

	if len(c8.kb.queue) > 0 {
		keys := []string{}
		for _, key := range c8.kb.queue {
			keys = append(keys, fmt.Sprintf("%d", key))
		}
		log.Printf("Unprocessed keys: %s", strings.Join(keys, " "))
	}

	err := c8.cpu.Tick(c8.mem, c8.vme, c8.audio, c8.kb)
	if err != nil {
		log.Fatal(err)
	}
}

func (c8 *Chip8) Draw(screen *ebiten.Image) {
	for x := 0; x < H_PIXELS; x++ {
		for y := 0; y < V_PIXELS; y++ {
			xor := c8.vme.mem[x][y] ^ c8.vme.buf[x][y]
			if xor == 1 {
				pixel := Pixel{x, y, bytob(c8.vme.buf[x][y])}
				pixel.Draw(screen)
			}
		}
	}
}

func bytob(value byte) bool {
	if value == 1 {
		return true
	} else {
		return false
	}
}

type Keyboard struct {
	queue []uint16
}

func (kb *Keyboard) Update() {
	// 0~9: 43~52
	for _, key := range inpututil.PressedKeys() {
		if (key >= 43 && key <= 52) || (key >= 0 && key <= 5) {
			kb.queue = append(kb.queue, uint16(keytohex(key)))
			// log.Printf("keyPressed=%d \n", key)
		}
	}
}

func NewKeyboard() *Keyboard {
	kb := new(Keyboard)
	kb.queue = []uint16{}
	return kb
}

func (kb *Keyboard) Pop() *uint16 {
	len := len(kb.queue)
	if len > 0 {
		key := kb.queue[0]
		kb.queue = kb.queue[1:]
		return &key
	} else {
		return nil
	}
}

func (kb *Keyboard) Clear() {
	kb.queue = []uint16{}
}

func keytohex(key ebiten.Key) uint16 {
	if key >= 43 && key <= 52 {
		return uint16(key) - 43
	} else {
		return uint16(key) + 0x10
	}
}

type Cpu struct {
	v     [64]uint8
	i     uint16
	stack [16]uint16
	sp    uint16
	pc    uint16
	dt    uint16
	st    uint16
	rnd   *rand.Rand
	lastd time.Time
	lasts time.Time
}

func NewCpu() *Cpu {
	cpu := new(Cpu)
	cpu.pc = 0x200
	cpu.rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	cpu.lastd = time.Now()
	cpu.lasts = time.Now()
	return cpu
}

func (cpu *Cpu) rand() uint8 {
	return uint8(cpu.rnd.Intn(256))
}

func (cpu *Cpu) Tick(mem *Memory, vme *VideoMemory, audio *audio.Player, kb *Keyboard) error {
	o1 := mem.buf[cpu.pc] >> 4
	o2 := mem.buf[cpu.pc] & 0x0F
	o3 := mem.buf[cpu.pc+1] >> 4
	o4 := mem.buf[cpu.pc+1] & 0x0F
	opcode := fmt.Sprintf("%02X%02X%02X%02X", o1, o2, o3, o4)
	log.Printf("Tick sp=%d pc=%d dt=%d st=%d opcode=%s", cpu.sp, cpu.pc, cpu.dt, cpu.st, opcode)

	nnn := (uint16(o2) << 8) + (uint16(o3) << 4) + uint16(o4)
	kk := (uint8(o3) << 4) + uint8(o4)
	x := o2
	y := o3
	vx := uint16(cpu.v[o2])
	vy := uint16(cpu.v[o3])
	xy := vx + vy

	var cmd Command
	switch o1 {
	case 0x0:
		switch o2 {
		case 0x0:
			switch o3 {
			case 0xE:
				switch o4 {
				case 0x0:
					log.Println("CLS")
					vme.clear()
					cmd = Next{}
				case 0xE:
					log.Println("00EE RET")
					pc := cpu.stack[cpu.sp-1]
					cpu.sp -= 1
					cmd = Jump{pc + 2}
				}
			}
		default:
			log.Println("SYS addr")
			cmd = Jump{nnn}
		}
	case 0x1:
		log.Println("1nnn JP")
		cmd = Jump{nnn}
	case 0x2:
		log.Println("2nnn CALL")
		cpu.stack[cpu.sp] = cpu.pc
		cpu.sp += 1
		cmd = Jump{nnn}
	case 0x3:
		log.Println("3xkk SE")
		if vx == uint16(kk) {
			cmd = Skip{}
		} else {
			cmd = Next{}
		}
	case 0x4:
		log.Println("4xkk SNE")
		if vx != uint16(kk) {
			cmd = Skip{}
		} else {
			cmd = Next{}
		}
	case 0x5:
		log.Println("5xy0 - SE")
		if vx == vy {
			cmd = Skip{}
		} else {
			cmd = Next{}
		}
	case 0x6:
		log.Println("6xkk - LD")
		cpu.v[x] = kk
		cmd = Next{}
	case 0x7:
		log.Println("7xkk - ADD")
		cpu.v[x] += kk
		cmd = Next{}
	case 0x8:
		switch o4 {
		case 0x0:
			log.Println("8xk0 - LD Vx, Vy")
			cpu.v[x] = cpu.v[y]
		case 0x1:
			log.Println("8xk1 - OR Vx, Vy")
			cpu.v[x] |= cpu.v[y]
		case 0x2:
			log.Println("8xk2 - AND Vx, Vy")
			cpu.v[x] &= cpu.v[y]
		case 0x3:
			log.Println("8xk3 - XOR Vx, Vy")
			cpu.v[x] ^= cpu.v[y]
		case 0x4:
			log.Println("8xk4 - ADD Vx, Vy")
			if xy > 0xFF {
				cpu.v[0xF] = 1
			} else {
				cpu.v[0xF] = 0
			}
			cpu.v[x] = uint8(xy & 0xFF)
		case 0x5:
			log.Println("8xk5 - SUB Vx, Vy")
			if vx > vy {
				cpu.v[0xF] = 1
			} else {
				cpu.v[0xF] = 0
			}
			cpu.v[x] = uint8(vx - vy)
		case 0x6:
			log.Println("8xk6 - SHR Vx, Vy")
			cpu.v[0xF] = uint8(vx & 0x1)
			cpu.v[x] /= 2
		case 0x7:
			log.Println("8xk7 - SUBN Vx, Vy")
			if vy > vx {
				cpu.v[0xF] = 1
			} else {
				cpu.v[0xF] = 0
			}
			cpu.v[x] = uint8(vy - vx)
		case 0xE:
			log.Println("8xkE - SHL Vx, Vy")
			cpu.v[0xF] = cpu.v[x] >> 7
			cpu.v[x] *= 2
		}
		cmd = Next{}
	case 0x9:
		log.Println("9xy0 - SNE")
		if vx != vy {
			cmd = Skip{}
		} else {
			cmd = Next{}
		}
	case 0xA:
		log.Println("Annn - LD I")
		cpu.i = nnn
		cmd = Next{}
	case 0xB:
		log.Println("Bnnn - JP")
		cmd = Jump{nnn + uint16(cpu.v[0])}
	case 0xC:
		log.Println("Cxkk - RND")
		cpu.v[x] = cpu.rand() & kk
		cmd = Next{}
	case 0xD:
		log.Println("DRW - Vx, Vy, nibble")
		n := o4
		bytes := mem.buf[cpu.i : cpu.i+uint16(n)]
		cpu.v[0xF] = vme.draw(vx, vy, bytes)
		cmd = Next{}
	case 0xE:
		switch o3 {
		case 0x9:
			log.Println("Ex9E - SKP")
			pressed := false
			for true {
				key := kb.Pop()
				if key == nil {
					break
				}
				if vx == *key {
					pressed = true
				}
			}
			if pressed {
				cmd = Skip{}
			} else {
				cmd = Next{}
			}
		case 0xA:
			log.Println("ExA1 - SKNP")
			pressed := false
			for true {
				key := kb.Pop()
				if key == nil {
					break
				}
				if vx == *key {
					pressed = true
				}
			}
			if !pressed {
				cmd = Skip{}
			} else {
				cmd = Next{}
			}
		}
	case 0xF:
		switch o3 {
		case 0x0:
			switch o4 {
			case 0x7:
				log.Println("Fx07 - LD Vx, DT")
				cpu.v[x] = uint8(cpu.dt)
				cmd = Next{}
			case 0xA:
				log.Println("Fx0A - LD Vx, K")
				key := kb.Pop()
				if key != nil {
					cpu.v[x] = uint8(*key)
					cmd = Next{}
				} else {
					// Do nothing.
				}
			}
		case 0x1:
			switch o4 {
			case 0x5:
				log.Println("Fx15 - LD DT")
				cpu.dt = vx
				cpu.lastd = time.Now()
				cmd = Next{}
			case 0x8:
				log.Println("Fx18 - LD ST")
				cpu.st = vx
				cpu.lasts = time.Now()
				cmd = Next{}
			case 0xE:
				log.Println("Fx1E - ADD I Vx")
				cpu.i += vx
				cmd = Next{}
			}
		case 0x2:
			log.Println("Fx29 - LD F")
			cpu.i = vx * 5
			cmd = Next{}
		case 0x3:
			log.Println("Fx33 - LD B")
			mem.buf[cpu.i] = (uint8(vx) / 100) % 10
			mem.buf[cpu.i+1] = (uint8(vx) / 10) % 10
			mem.buf[cpu.i+2] = uint8(vx) % 10
			cmd = Next{}
		case 0x5:
			log.Println("Fx55 - LD [I]")
			for n := 0; n <= int(x); n++ {
				mem.buf[cpu.i+uint16(n)] = cpu.v[n]
			}
			cmd = Next{}
		case 0x6:
			log.Println("Fx65 - LD")
			for n := 0; n <= int(x); n++ {
				cpu.v[n] = mem.buf[cpu.i+uint16(n)]
			}
			cmd = Next{}
		}
	}

	if cmd != nil {
		cmd.exec(cpu)
	}

	now := time.Now()
	elapsed := now.Sub(cpu.lastd)
	if elapsed.Seconds() > 1.0/60 && cpu.dt > 0 {
		cpu.dt -= 1
		cpu.lastd = now
	}

	elapsed = now.Sub(cpu.lasts)
	if elapsed.Seconds() > 1.0/60 && cpu.st > 0 {
		audio.Play()
		audio.Rewind()
		cpu.st -= 1
		cpu.lasts = now
	}

	return nil
}

type Command interface {
	exec(cpu *Cpu)
}

type Next struct{}

func (c Next) exec(cpu *Cpu) {
	cpu.pc += 2
}

type Jump struct {
	addr uint16
}

func (c Jump) exec(cpu *Cpu) {
	cpu.pc = c.addr
}

type Skip struct{}

func (c Skip) exec(cpu *Cpu) {
	cpu.pc += 4
}

type Memory struct {
	buf [0xFFF]byte // Chip-8 has 0xFFFF (4096) bytes of RAM.
}

func (m *Memory) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}

	n, err := f.Read(m.buf[0x200:])
	log.Printf("%d bytes read from \"%s\".", n, path)
	return nil
}

func NewMemory() *Memory {
	m := new(Memory)

	// Load fontsets.
	m.buf = [0xFFF]byte{0xF0, 0x90, 0x90, 0x90, 0xF0, 0x20, 0x60, 0x20, 0x20, 0x70, 0xF0, 0x10, 0xF0, 0x80, 0xF0, 0xF0, 0x10, 0xF0, 0x10, 0xF0, 0x90, 0x90, 0xF0, 0x10, 0x10, 0xF0, 0x80, 0xF0, 0x10, 0xF0, 0xF0, 0x80, 0xF0, 0x90, 0xF0, 0xF0, 0x10, 0x20, 0x40, 0x40, 0xF0, 0x90, 0xF0, 0x90, 0xF0, 0xF0, 0x90, 0xF0, 0x10, 0xF0, 0xF0, 0x90, 0xF0, 0x90, 0x90, 0xE0, 0x90, 0xE0, 0x90, 0xE0, 0xF0, 0x80, 0x80, 0x80, 0xF0, 0xE0, 0x90, 0x90, 0x90, 0xE0, 0xF0, 0x80, 0xF0, 0x80, 0xF0, 0xF0, 0x80, 0xF0, 0x80, 0x80}

	return m
}

// VideoMemory implements double buffer.
type VideoMemory struct {
	buf [H_PIXELS][V_PIXELS]byte
	mem [H_PIXELS][V_PIXELS]byte
}

func NewVideoMemory() *VideoMemory {
	return new(VideoMemory)
}

func (vme *VideoMemory) clear() {
	for x := 0; x < H_PIXELS; x++ {
		for y := 0; y < V_PIXELS; y++ {
			vme.buf[x][y] = 0
		}
	}
}

func (vme *VideoMemory) draw(x uint16, y uint16, buf []byte) uint8 {
	vf := uint16(0)
	for i, byte := range buf {
		i := uint16(i)
		vf += vme.draw_pixcel(x, y+i, (byte>>7)&0x1)
		vf += vme.draw_pixcel(x+1, y+i, (byte>>6)&0x1)
		vf += vme.draw_pixcel(x+2, y+i, (byte>>5)&0x1)
		vf += vme.draw_pixcel(x+3, y+i, (byte>>4)&0x1)
		vf += vme.draw_pixcel(x+4, y+i, (byte>>3)&0x1)
		vf += vme.draw_pixcel(x+5, y+i, (byte>>2)&0x1)
		vf += vme.draw_pixcel(x+6, y+i, (byte>>1)&0x1)
		vf += vme.draw_pixcel(x+7, y+i, (byte>>0)&0x1)
	}

	if vf > 0 {
		return 1
	} else {
		return 0
	}
}

func (vme *VideoMemory) draw_pixcel(x uint16, y uint16, new byte) uint16 {
	var vf uint16

	// Check collision.
	if vme.buf[x][y] == 1 && new == 1 {
		vf = 1
	} else {
		vf = 0
	}

	vme.buf[x][y] ^= new
	return vf
}

type Button struct {
	text      string
	img       *ebiten.Image
	x         int
	y         int
	onclicked func(*Button)
	font      *font.Face
	rom       Rom
}

func NewButton(text string, font *font.Face, x, y int, rom Rom, onclicked func(*Button)) *Button {
	btn := new(Button)
	img := ebiten.NewImage(BUTTON_WIDTH-1, BUTTON_HIGHT-1)
	img.Fill(color.White)
	btn.img = img
	btn.text = text
	btn.font = font
	btn.x = x
	btn.y = y
	btn.rom = rom
	btn.onclicked = onclicked
	return btn
}

func (btn *Button) Draw(screen *ebiten.Image) {
	opts := &ebiten.DrawImageOptions{}
	opts.GeoM.Translate(float64(btn.x*BUTTON_WIDTH), float64(btn.y*BUTTON_HIGHT)+float64(SELECT_HIGHT))
	screen.DrawImage(btn.img, opts)

	text.Draw(screen, btn.text, *btn.font, btn.x*BUTTON_WIDTH+10, btn.y*BUTTON_HIGHT+17+SELECT_HIGHT, color.Black)
}

type UI struct {
	btns        []*Button
	oncompleted func(rom Rom)
	font        *font.Face
}

func (ui *UI) Draw(screen *ebiten.Image) {
	text.Draw(screen, "SELECT A GAME", *ui.font, 160, 36, color.White)
	for _, btn := range ui.btns {
		btn.Draw(screen)
	}
}

func (ui *UI) Update() {
	clicked := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	x, y := ebiten.CursorPosition()
	if clicked {
		log.Printf("Clicked: %v on (%d, %d)", clicked, x, y)
		for _, btn := range ui.btns {
			minx := btn.img.Bounds().Min.X + btn.x*BUTTON_WIDTH
			maxx := btn.img.Bounds().Max.X + btn.x*BUTTON_WIDTH
			miny := btn.img.Bounds().Min.Y + btn.y*BUTTON_HIGHT + SELECT_HIGHT
			maxy := btn.img.Bounds().Max.Y + btn.y*BUTTON_HIGHT + SELECT_HIGHT
			log.Printf("x=%d y=%d minx=%d maxx=%d miny=%d maxy=%d", x, y, minx, maxx, miny, maxy)
			if x >= minx && x <= maxx && y >= miny && y <= maxy {
				btn.onclicked(btn)
			}
		}
	}
}

type Rom struct {
	name string
	path string
}

func NewUI() *UI {
	ROMS := [90]Rom{
		{"15Puzzle", "roms/15 Puzzle [Roger Ivie].ch8"},
		{"Addition Problems", "roms/Addition Problems [Paul C. Moews].ch8"},
		{"Airplane", "roms/Airplane.ch8"},
		{"Animal Race", "roms/Animal Race [Brian Astle].ch8"},
		{"Astro Dodge", "roms/Astro Dodge [Revival Studios, 2008].ch8"},
		{"BMP Viewer", "roms/BMP Viewer - Hello (C8 example) [Hap, 2005].ch8"},
		{"Biorhythm ", "roms/Biorhythm [Jef Winsor].ch8"},
		{"Blinky", "roms/Blinky [Hans Christian Egeberg, 1991].ch8"},
		{"Blitz ", "roms/Blitz [David Winter].ch8"},
		{"Bowling", "roms/Bowling [Gooitzen van der Wal].ch8"},
		{"Breakout", "roms/Breakout (Brix hack) [David Winter, 1997].ch8"},
		{"Brick", "roms/Brick (Brix hack, 1990).ch8"},
		{"Brix", "roms/Brix [Andreas Gustafsson, 1990].ch8"},
		{"Cave", "roms/Cave.ch8"},
		{"Chip8 Picture", "roms/Chip8 Picture.ch8"},
		{"Chip8 Logo", "roms/Chip8 emulator Logo [Garstyciuks].ch8"},
		{"Clock Program", "roms/Clock Program [Bill Fisher, 1981].ch8"},
		{"Coin Flipping", "roms/Coin Flipping [Carmelo Cortez, 1978].ch8"},
		{"Connect 4", "roms/Connect 4 [David Winter].ch8"},
		{"Craps", "roms/Craps [Camerlo Cortez, 1978].ch8"},
		{"Deflection", "roms/Deflection [John Fort].ch8"},
		{"Delay Timer Test", "roms/Delay Timer Test [Matthew Mikolay, 2010].ch8"},
		{"Division Test", "roms/Division Test [Sergey Naydenov, 2010].ch8"},
		{"Figures", "roms/Figures.ch8"},
		{"Filter", "roms/Filter.ch8"},
		{"Fishie", "roms/Fishie [Hap, 2005].ch8"},
		{"Framed", "roms/Framed MK1 [GV Samways, 1980].ch8"},
		{"Framed2", "roms/Framed MK2 [GV Samways, 1980].ch8"},
		{"Guess", "roms/Guess [David Winter].ch8"},
		{"Hi-Lo", "roms/Hi-Lo [Jef Winsor, 1978].ch8"},
		{"Hidden", "roms/Hidden [David Winter, 1996].ch8"},
		{"IBM Logo", "roms/IBM Logo.ch8"},
		{"Jumping X", "roms/Jumping X and O [Harry Kleinberg, 1977].ch8"},
		{"Kaleidoscope", "roms/Kaleidoscope [Joseph Weisbecker, 1978].ch8"},
		{"Keypad Test", "roms/Keypad Test [Hap, 2006].ch8"},
		{"Landing", "roms/Landing.ch8"},
		{"Life", "roms/Life [GV Samways, 1980].ch8"},
		{"Lunar Lander", "roms/Lunar Lander (Udo Pernisz, 1979).ch8"},
		{"Mastermind FourRow", "roms/Mastermind FourRow (Robert Lindley, 1978).ch8"},
		{"Maze", "roms/Maze [David Winter, 199x].ch8"},
		{"Merlin", "roms/Merlin [David Winter].ch8"},
		{"Minimal", "roms/Minimal game [Revival Studios, 2007].ch8"},
		{"Missile", "roms/Missile [David Winter].ch8"},
		{"Most", "roms/Most Dangerous Game [Peter Maruhnic].ch8"},
		{"Nim ", "roms/Nim [Carmelo Cortez, 1978].ch8"},
		{"Paddles", "roms/Paddles.ch8"},
		{"Particle ", "roms/Particle Demo [zeroZshadow, 2008].ch8"},
		{"Pong", "roms/Pong (1 player).ch8"},
		{"Pong 2", "roms/Pong 2 (Pong hack) [David Winter, 1997].ch8"},
		{"Pong 3", "roms/Pong [Paul Vervalin, 1990].ch8"},
		{"Programmable Spacefighters", "roms/Programmable Spacefighters [Jef Winsor].ch8"},
		{"Puzzle", "roms/Puzzle.ch8"},
		{"Random Number", "roms/Random Number Test [Matthew Mikolay, 2010].ch8"},
		{"Reversi", "roms/Reversi [Philip Baltzer].ch8"},
		{"Rocket Launch", "roms/Rocket Launch [Jonas Lindstedt].ch8"},
		{"Rocket Launcher", "roms/Rocket Launcher.ch8"},
		{"Rocket ", "roms/Rocket [Joseph Weisbecker, 1978].ch8"},
		{"Rush Hour", "roms/Rush Hour [Hap, 2006].ch8"},
		{"Russian Roulette", "roms/Russian Roulette [Carmelo Cortez, 1978].ch8"},
		{"SQRT Test", "roms/SQRT Test [Sergey Naydenov, 2010].ch8"},
		{"Sequence Shoot", "roms/Sequence Shoot [Joyce Weisbecker].ch8"},
		{"Shooting Stars", "roms/Shooting Stars [Philip Baltzer, 1978].ch8"},
		{"Sierpinski", "roms/Sierpinski [Sergey Naydenov, 2010].ch8"},
		{"Slide ", "roms/Slide [Joyce Weisbecker].ch8"},
		{"Soccer", "roms/Soccer.ch8"},
		{"Space Flight", "roms/Space Flight.ch8"},
		{"Space Intercept", "roms/Space Intercept [Joseph Weisbecker, 1978].ch8"},
		{"Space Invaders", "roms/Space Invaders [David Winter].ch8"},
		{"Spooky Spot", "roms/Spooky Spot [Joseph Weisbecker, 1978].ch8"},
		{"Squash", "roms/Squash [David Winter].ch8"},
		{"Stars", "roms/Stars [Sergey Naydenov, 2010].ch8"},
		{"Submarine", "roms/Submarine [Carmelo Cortez, 1978].ch8"},
		{"Sum Fun", "roms/Sum Fun [Joyce Weisbecker].ch8"},
		{"Syzygy", "roms/Syzygy [Roy Trevino, 1990].ch8"},
		{"Tank", "roms/Tank.ch8"},
		{"Tapeworm", "roms/Tapeworm [JDR, 1999].ch8"},
		{"Tetris", "roms/Tetris [Fran Dachille, 1991].ch8"},
		{"Tic-Tac-Toe", "roms/Tic-Tac-Toe [David Winter].ch8"},
		{"Timebomb", "roms/Timebomb.ch8"},
		{"Trip8 Demo", "roms/Trip8 Demo (2008) [Revival Studios].ch8"},
		{"Tron", "roms/Tron.ch8"},
		{"UFO", "roms/UFO [Lutz V, 1992].ch8"},
		{"Vers", "roms/Vers [JMN, 1991].ch8"},
		{"Vertical Brix", "roms/Vertical Brix [Paul Robson, 1996].ch8"},
		{"Wall", "roms/Wall [David Winter].ch8"},
		{"Wipe Off", "roms/Wipe Off [Joseph Weisbecker].ch8"},
		{"Worm V4", "roms/Worm V4 [RB-Revival Studios, 2007].ch8"},
		{"X-Mirror", "roms/X-Mirror.ch8"},
		{"Zero Demo", "roms/Zero Demo [zeroZshadow, 2007].ch8"},
		{"ZeroPong ", "roms/ZeroPong [zeroZshadow, 2007].ch8"},
	}

	ui := new(UI)

	tt, err := opentype.Parse(PressStart2P_ttf)
	if err != nil {
		log.Fatal(err)
	}

	titleFont, err := opentype.NewFace(tt, &opentype.FaceOptions{
		Size:    24,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	btnFont, err := opentype.NewFace(tt, &opentype.FaceOptions{
		Size:    7,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	ui.font = &titleFont

	cb := func(btn *Button) {
		log.Printf("button %s was clicked!", btn.text)
		if ui.oncompleted != nil {
			ui.oncompleted(btn.rom)
		}
	}

	for n, rom := range ROMS {
		x := n % 8
		y := n / 8
		ui.btns = append(ui.btns, NewButton(rom.name, &btnFont, x, y, rom, cb))
	}

	return ui
}

// Workaround to create a variable to receive both UI and Chip8 object.
type Scene interface {
	Draw(screen *ebiten.Image)
	Update()
}

type Game struct {
	scene Scene
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return 640, 320
}

func (g *Game) Draw(screen *ebiten.Image) {
	g.scene.Draw(screen)
	ebitenutil.DebugPrint(screen, fmt.Sprintf("%f", ebiten.CurrentTPS()))
}

func (g *Game) Update() error {
	g.scene.Update()
	return nil
}

func main() {
	ebiten.SetMaxTPS(800)
	ebiten.SetWindowSize(640, 320)
	ebiten.SetWindowTitle("CHIP-8")
	cpu := NewCpu()
	mem := NewMemory()
	vme := NewVideoMemory()

	f, err := os.Open("audio.mp3")
	if err != nil {
		log.Fatal(err)
	}
	audio, err := audio.NewPlayer(audio.NewContext(32000), f)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%+v", mem)

	kb := NewKeyboard()

	ui := NewUI()

	c8 := Chip8{cpu, mem, vme, audio, kb}

	game := Game{ui}
	ui.oncompleted = func(rom Rom) {
		game.scene = &c8
		err := c8.mem.Load(rom.path)
		if err != nil {
			panic(err)
		}
	}
	if err := ebiten.RunGame(&game); err != nil {
		log.Fatal(err)
	}

}
