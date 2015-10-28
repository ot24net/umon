package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	fs "gopkg.in/fsnotify.v1"
)

//
// Event
//
type Event struct {
	E fs.Event
	T time.Time
	I os.FileInfo
}

//
// chans
//
type ChanEvent chan *Event
type ChanExit chan bool

//
// Piper
//
type Piper interface {
	Pipe(next Piper) Piper
	Handle(e *Event)
	Close() error
}

//
// Watcher watch file changes
//
type Watcher struct {
	w    *fs.Watcher
	next Piper
	exit ChanExit
}

// NewWatcher create watcher
func NewWatcher(dirs []string) (*Watcher, error) {
	// w
	w, err := fs.NewWatcher()
	if err != nil {
		return nil, err
	}

	// listen
	log.Println("[umon]", "Watcher: watch dirs", dirs)
	for _, dir := range dirs {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if info == nil {
				return err
			}
			if !info.IsDir() {
				return nil
			}
			w.Add(path)
			return nil
		})
	}

	// ok
	return &Watcher{
		w:    w,
		exit: make(ChanExit, 1),
	}, nil
}

// @impl Piper
func (w *Watcher) Pipe(next Piper) Piper {
	w.next = next
	go w.runWatch()
	return next
}

// runWatch run watch
func (w *Watcher) runWatch() {
	for {
		select {
		case <-w.exit:
			return

		case e := <-w.w.Events:
			info, _ := os.Stat(e.Name)
			if info != nil && info.IsDir() {
				switch e.Op {
				case fs.Create:
					w.w.Add(e.Name)
				case fs.Remove:
					w.w.Remove(e.Name)
				}
			}
			w.next.Handle(&Event{
				E: e,
				T: time.Now(),
				I: info,
			})
		}
	}
}

// @impl Piper.Handle
func (w *Watcher) Handle(e *Event) {
	// empty
}

// Close watch
func (w *Watcher) Close() error {
	w.exit <- true
	return nil
}

//
// Filter filter unwanted event
//
type Filter struct {
	next Piper

	events ChanEvent
	exit   ChanExit
}

func NewFilter() (*Filter, error) {
	return &Filter{
		exit:   make(ChanExit, 1),
		events: make(ChanEvent, 16),
	}, nil
}

// @impl Piper.Handle
func (f *Filter) Handle(e *Event) {
	f.events <- e
}

// @impl Piper.Pipe
func (f *Filter) Pipe(next Piper) Piper {
	f.next = next
	go f.runFilt()
	return next
}

// runFilter run filter
func (f *Filter) runFilt() {
	// last time when receive event
	var (
		lastTime time.Time
		lastEvt  *Event
	)

	// check event ticker
	ticker := time.NewTicker(time.Second)

	for {
		select {
		case <-f.exit:
			return
			
		case e := <-f.events:
			name := filepath.Base(e.E.Name)
			if strings.HasPrefix(name, ".") {
				continue
			}
			ext := filepath.Ext(name)
			if ext != ".go" {
				continue
			}
			lastTime = time.Now()
			lastEvt = e

		case <-ticker.C:
			if lastEvt == nil {
				continue
			}
			if time.Now().Sub(lastTime) < time.Second {
				continue
			}
			f.next.Handle(lastEvt)
			lastEvt = nil
		}
	}
}

// @impl Close
func (f *Filter) Close() error {
	f.exit <- true
	return nil
}

//
// Builder builder unwanted event
//
type Builder struct {
	next   Piper
	events ChanEvent
	exit   ChanExit
}

func NewBuilder() (*Builder, error) {
	return &Builder{
		exit:   make(ChanExit, 1),
		events: make(ChanEvent, 16),
	}, nil
}

// @impl Piper.Handle
func (b *Builder) Handle(e *Event) {
	b.events <- e
}

// @impl Piper.Pipe
func (b *Builder) Pipe(next Piper) Piper {
	b.next = next
	go b.runBuild()
	return next
}

// runBuild run builder
func (b *Builder) runBuild() {
	var (
		lastTime time.Time
		lastEvt  *Event
	)
	ticker := time.NewTicker(time.Second)

	for {
		select {
		case <-b.exit:
			return

		case e := <-b.events:
			cmd := exec.Command("go", "build")
			cmd.Stderr, cmd.Stdout = os.Stderr, os.Stdout
			if err := cmd.Start(); err != nil {
				log.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				log.Println("[umon]", "Builder: build err", err)
			} else {
				lastTime = time.Now()
				lastEvt = e
				log.Println("[umon]", "Builder: build ok")
			}
			
		case <-ticker.C:
			if lastEvt == nil {
				continue
			}
			if time.Now().Sub(lastTime) < time.Second {
				continue
			}
			b.next.Handle(lastEvt)
			lastEvt = nil
		}
	}
}

// @impl Close
func (b *Builder) Close() error {
	b.exit <- true
	return nil
}

//
// Runner runner unwanted event
//
type Runner struct {
	next   Piper
	events ChanEvent
	exit   ChanExit
}

func NewRunner() (*Runner, error) {
	return &Runner{
		exit:   make(ChanExit, 1),
		events: make(ChanEvent, 16),
	}, nil
}

// @impl Piper.Handle
func (b *Runner) Handle(e *Event) {
	b.events <- e
}

// @impl Piper.Pipe
func (b *Runner) Pipe(next Piper) Piper {
	b.next = next
	go b.runRun()
	return next
}

// runRun run runner
func (b *Runner) runRun() {
	// app name
	runDir := os.Getenv("PWD")
	if len(runDir) == 0 {
		panic("no PWD env")
	}
	appName := filepath.Join(runDir, filepath.Base(runDir))
	log.Println("[umon]", "Runner: app", appName)

	// notify run
	runC := make(chan bool, 1)
	runC <- true

	// cmd
	var cmd *exec.Cmd
	for {
		select {
		case <-b.exit:
			return

		case <-b.events:
			runC <- true
			
		case <-runC:
			// kill
			if cmd != nil {
				log.Println("[umon]", "Runner: kill", cmd.Process)
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
				cmd = nil
				time.Sleep(200*time.Microsecond)
			}

			// exist
			if _, err := os.Stat(appName); err != nil {
				if os.IsNotExist(err) {
					log.Println("[umon]", "Runner: app not exist, run go build first")
				}
				continue
			}

			// start
			cmd = exec.Command(appName)
			cmd.Stderr, cmd.Stdout = os.Stderr, os.Stdout
			if err := cmd.Start(); err != nil {
				log.Println("[umon]", "Runner: start err", err)
			} else {
				log.Println("[umon]", "Runner: start ok")
			}
		}
	}
}

// @impl Close
func (b *Runner) Close() error {
	b.exit <- true
	return nil
}

//
// Ender
//
type Ender struct {
	// empty
}

// NewEnder create ender instance
func NewEnder() (*Ender, error) {
	return &Ender{
	// empty
	}, nil
}

// @impl Piper.Pipe
func (ed *Ender) Pipe(next Piper) Piper {
	// no next
	return nil
}

// @impl Piper.Handle
func (ed *Ender) Handle(e *Event) {
	log.Println("[umon]", "Ender: handle", e)
}

// @impl Close
func (ed *Ender) Close() error {
	return nil
}

//
// umon ../app ../ctrl
//
func main() {
	// watcher
	watchDir := []string{"."}
	if len(os.Args) > 1 {
		watchDir = os.Args[1:]
	}
	watch, err := NewWatcher(watchDir)
	if err != nil {
		log.Fatal(err)
	}
	defer watch.Close()

	// filter
	filter, err := NewFilter()
	if err != nil {
		log.Fatal(err)
	}
	defer filter.Close()

	// builder
	builder, err := NewBuilder()
	if err != nil {
		log.Fatal(err)
	}
	defer builder.Close()

	// runner
	runner, err := NewRunner()
	if err != nil {
		log.Fatal(err)
	}
	defer runner.Close()

	// end
	end, err := NewEnder()
	if err != nil {
		log.Fatal(err)
	}
	defer end.Close()

	// pipe
	watch.Pipe(filter).Pipe(builder).Pipe(runner).Pipe(end)

	// wait
	exit := make(ChanExit)
	<-exit
}
