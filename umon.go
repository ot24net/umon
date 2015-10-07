package main

import (
	"log"
	"os"
	"time"
	"strings"
	"os/exec"
	"path/filepath"
	
	fs "gopkg.in/fsnotify.v1"
)

//
// Get umon execute folder
//
func ExecFolder() (string, error) {
	p, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return "", err
	}
	
	f, _ := filepath.Split(filepath.Clean(p))
	if len(f) > 1 {
		for i := len(f)-2; i >= 0; i-- {
			if f[i] == '/' {
				return f[i+1:len(f)-1], nil
			}
		}
	}
	
	return f, nil
}

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
	w *fs.Watcher
	next Piper
	exit ChanExit
}

// NewWatcher create watcher
func NewWatcher(dir string) (*Watcher, error) {
	// w
	w, err := fs.NewWatcher()
	if err != nil {
		return nil, err
	}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info == nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		//log.Println(path)
		w.Add(path)
		return nil
	})
	
	// ok
	return &Watcher {
		w: w,
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
			//log.Println("Watcher:", e.String())
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
	exit ChanExit
}

func NewFilter() (*Filter, error) {
	return &Filter{
		exit: make(ChanExit, 1),
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
		lastEvt *Event
	)
	
	// check event ticker
	ticker := time.NewTicker(1*time.Second)
	
	for {
		select {
		case <-f.exit:
			return
			
		case <-ticker.C:
			if lastEvt == nil {
				continue
			}
			if time.Now().Sub(lastTime) < time.Second {
				continue
			}
			// log.Println("Filter pass:", filepath.Base(lastEvt.E.Name))
			f.next.Handle(lastEvt)
			lastEvt = nil
			
		case e := <-f.events:
			//log.Println("Filter:", e)
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
	next Piper
	events ChanEvent
	exit ChanExit
}

func NewBuilder() (*Builder, error) {
	return &Builder{
		exit: make(ChanExit, 1),
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
		lastEvt *Event
	)
	ticker := time.NewTicker(1*time.Second)
	
	for {
		select {
		case <-b.exit:
			return
			
		case <-ticker.C:
			if lastEvt == nil {
				continue
			}
			if time.Now().Sub(lastTime) < time.Second {
				continue
			}
			b.next.Handle(lastEvt)
			lastEvt = nil
			
		case e := <-b.events:
			// log.Println("Builder:", e)
			cmd := exec.Command("go", "build")
			cmd.Stderr, cmd.Stdout = os.Stderr, os.Stdout
			if err := cmd.Start(); err != nil {
				log.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				log.Println("build FAIL")
			} else {
				lastTime = time.Now()
				lastEvt = e
				log.Println("buid SUCCESS:", e)
			}
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
	next Piper
	events ChanEvent
	exit ChanExit
}

func NewRunner() (*Runner, error) {
	return &Runner{
		exit: make(ChanExit, 1),
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
	var cmd *exec.Cmd
	appName := "./" + ExecFolder()
	
	runC := make(chan bool, 1)
	runC <- true

	for {
		select {
		case <-b.exit:
			return
			
		case <-runC:
			// kill
			if cmd != nil {
				log.Println("kill:", cmd.Process)
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
				cmd = nil
				time.Sleep(time.Second)
			} else {
				log.Println("cmd == nil") 
			}
			
			// exist
			if _, err := os.Stat(appName); err != nil {
				continue
			}

			// start
			cmd = exec.Command(appName)
			cmd.Stderr, cmd.Stdout = os.Stderr, os.Stdout
			if err := cmd.Start(); err != nil {
				log.Println("Runner err:", err)
			} else {
				log.Println("Runner success")
			}
			
		case e := <-b.events:
			log.Println("Runner:", e)
			runC <- true
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
	log.Println("Ender:", e)
}

// @impl Close
func (ed *Ender) Close() error {
	return nil
}

//
// main
//
func main() {
	// watcher
	watch, err := NewWatcher(".")
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
	
	// exit
	exit := make(ChanExit)
	<-exit
}