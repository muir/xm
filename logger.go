package xm

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/muir/xm/z"
)

type Log struct {
	seed   Seed
	local  local
	shared *shared
}

type local struct {
	ForkCounter    int32
	StepCounter    int32
	Created        time.Time
	InDirty        int32      // in shared.Dirty? 0 = false, 1 = true
	DataLock       sync.Mutex // protects Data
	Data           map[string]interface{}
	IsBufferParent bool
}

// shared is common between the loggers that share a search index
type shared struct {
	RefCount      int32
	DataLock      sync.Mutex // protects Data, Index and Dirty, can take local.DataLock while holding this lock
	Data          map[string]interface{}
	Index         map[string][]string
	UnflushedLogs int32
	FlushTimer    *time.Timer
	FlushDelay    time.Duration
	FlushActive   int32 // 1 == timer is running, 0 = timer is not running

	// Dirty holds spans that have modified data and need to be
	// written or re-written.  It does not not track logs that
	// need flushing
	Dirty []*Log
}

var DefaultFlushDelay = time.Minute * 5

func (s Seed) Log(description string) *Log {
	s = s.Copy()
	s.myTrace.rebuildSetNonZero()
	log := &Log{
		seed: s,
		shared: &shared{
			RefCount:    1,
			Data:        copyMap(s.data),
			FlushActive: 1,
			Index:       make(map[string][]string),
		},
		local: local{
			InDirty:        1,
			Created:        time.Now(),
			IsBufferParent: true,
		},
	}
	log.shared.Dirty = append(log.shared.Dirty, log)
	log.finishBaseLoggerChanges()
	log.shared.FlushTimer = time.AfterFunc(DefaultFlushDelay, log.timerFlush)
	return log
}

func (old *Log) newChildLog(seed Seed) *Log {
	log := &Log{
		local: local{
			InDirty:        1,
			Created:        time.Now(),
			IsBufferParent: false,
			Data:           make(map[string]interface{}),
		},
		seed:   seed,
		shared: old.shared,
	}
	log.shared.DataLock.Lock()
	defer log.shared.DataLock.Unlock()
	log.shared.Dirty = append(log.shared.Dirty, log)
	return log
}

func (l *Log) touched() {
	wasInDirty := atomic.SwapInt32(&l.local.InDirty, 1)
	if wasInDirty == 0 {
		func() {
			l.shared.DataLock.Lock()
			defer l.shared.DataLock.Unlock()
			l.shared.Dirty = append(l.shared.Dirty, l)
			if len(l.shared.Dirty) == 1 {
				l.enableFlushTimer()
			}
		}()
	}
}

func (l *Log) enableFlushTimer() {
	was := atomic.SwapInt32(&l.shared.FlushActive, 1)
	if was == 0 {
		l.shared.FlushTimer.Reset(l.shared.FlushDelay)
	}
}

func (l *Log) timerFlush() {
	atomic.StoreInt32(&l.shared.FlushActive, 0)
	l.Flush()
}

func (l *Log) Flush() {
	atomic.StoreInt32(&l.shared.UnflushedLogs, 0)
	func() {
		l.shared.DataLock.Lock()
		defer l.shared.DataLock.Unlock()
		for _, dirtyLog := range l.shared.Dirty {
			atomic.StoreInt32(&dirtyLog.local.InDirty, 0)
			var index map[string][]string
			var data map[string]interface{}
			if dirtyLog.local.IsBufferParent {
				index = dirtyLog.shared.Index
				data = dirtyLog.shared.Data
			} else {
				func() {
					dirtyLog.local.DataLock.Lock()
					defer dirtyLog.local.DataLock.Unlock()
					data = dirtyLog.local.Data
				}()
			}
			for _, baseLogger := range l.seed.baseLoggers.List {
				baseLogger.Buffered.Span(
					dirtyLog.seed.description,
					dirtyLog.seed.myTrace,
					dirtyLog.seed.parentTrace,
					index,
					data)
			}
		}
		l.shared.Dirty = l.shared.Dirty[:0]
	}()
	for _, baseLogger := range l.seed.baseLoggers.List {
		baseLogger.Buffered.Flush()
	}
}

func (l *Log) log(level Level, msg string, values []z.Field) {
	unflushed := atomic.AddInt32(&l.shared.UnflushedLogs, 1)
	if unflushed == 1 {
		l.enableFlushTimer()
	}
	for _, baseLogger := range l.seed.baseLoggers.List {
		baseLogger.Prefilled.Log(level, msg, values)
	}
}

// XXX func (l *Log) Zap() zaplike.Log
// XXX func (l *Log) ZapSugar() zaplike.Sugar
// XXX func (l *Log) Zero() zerolike.Log

// End is used to single that a Log, Fork(), or Step() is done.  When all
// of the parts of a buffered log are finished, it is automatically flushed.
func (l *Log) End() {
	remaining := atomic.AddInt32(&l.shared.RefCount, -1)
	if remaining <= 0 {
		l.Flush()
	}
}

func (l *Log) addRef() {
	remaining := atomic.AddInt32(&l.shared.RefCount, 1)
	if remaining > 1 {
		return
	}
	// This indicates a bug in the code that is using the
	// logger.
	l.Warn("Too many calls to log.End(), log.EndFork(), or log.EndStep()")
	l.shared.FlushTimer.Reset(DefaultFlushDelay)
}

func (l *Log) Fork(msg string, mods ...SeedModifier) *Log {
	l.addRef()
	return l.ForkNoWait(msg, mods...)
}

func (l *Log) ForkNoWait(msg string, mods ...SeedModifier) *Log {
	seed := l.CopySeed(mods...).SubSpan()
	counter := int(atomic.AddInt32(&l.local.ForkCounter, 1))
	seed.prefix += "." + base26(counter)
	return l.newChildLog(seed)
}

func (l *Log) Step(msg string, mods ...SeedModifier) *Log {
	l.addRef()
	return l.StepNoWait(msg, mods...)
}

func (l *Log) StepNoWait(msg string, mods ...SeedModifier) *Log {
	seed := l.CopySeed(mods...).SubSpan()
	counter := int(atomic.AddInt32(&l.local.StepCounter, 1))
	seed.prefix += "." + strconv.Itoa(counter)
	return l.newChildLog(seed)
}

func (l *Log) BufferedSpanData(dataToAppend map[string]interface{}) {
	func() {
		l.shared.DataLock.Lock()
		defer l.shared.DataLock.Unlock()
		for key, value := range dataToAppend {
			l.local.Data[key] = value
		}
	}()
	l.touched()
}

func (l *Log) LocalSpanData(dataToAppend map[string]interface{}) {
	if l.local.IsBufferParent {
		l.BufferedSpanData(dataToAppend)
		return
	}
	func() {
		l.local.DataLock.Lock()
		defer l.local.DataLock.Unlock()
		for key, value := range dataToAppend {
			l.local.Data[key] = value
		}
	}()
	l.touched()
}

func (l *Log) SpanIndex(keyValuePairs ...string) {
	func() {
		l.shared.DataLock.Lock()
		defer l.shared.DataLock.Unlock()
		for i := 0; i < len(keyValuePairs)-2; i += 2 {
			key := keyValuePairs[i]
			l.shared.Index[key] = append(l.shared.Index[key], keyValuePairs[i+1])
		}
	}()
	l.touched()
}

func (l *Log) Debug(msg string, values ...z.Field) { l.log(DebugLevel, msg, values) }
func (l *Log) Trace(msg string, values ...z.Field) { l.log(TraceLevel, msg, values) }
func (l *Log) Info(msg string, values ...z.Field)  { l.log(InfoLevel, msg, values) }
func (l *Log) Warn(msg string, values ...z.Field)  { l.log(WarnLevel, msg, values) }
func (l *Log) Error(msg string, values ...z.Field) { l.log(ErrorLevel, msg, values) }
func (l *Log) Alert(msg string, values ...z.Field) { l.log(AlertLevel, msg, values) }

// XXX
// func (l *Log) Guage(name string, value float64, )
// func (l *Log) AdjustCounter(name string, value float64, )
// XXX redaction

func (l *Log) CurrentPrefill() []z.Field {
	c := make([]z.Field, len(l.seed.prefill))
	copy(c, l.seed.prefill)
	return c
}

func copyMap(o map[string]interface{}) map[string]interface{} {
	n := make(map[string]interface{})
	for k, v := range o {
		n[k] = v
	}
	return n
}