// This file is generated, DO NOT EDIT.  It comes from the corresponding .zzzgo file

package xoptest

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/muir/xop-go"
	"github.com/muir/xop-go/trace"
	"github.com/muir/xop-go/xopbase"
	"github.com/muir/xop-go/xopconst"
	"github.com/muir/xop-go/xoputil"
)

//go:generate enumer -type=EventType -linecomment -json -sql

type EventType int

const (
	LineEvent    EventType = iota // line
	RequestStart                  // requestStart
	RequestDone                   // requestDone
	SpanStart                     // spanStart
	SpanDone                      // spanStart
	FlushEvent                    // flush
	MetadataSet                   // metadata
	CustomEvent                   // custom
)

type testingT interface {
	Log(...interface{})
	Name() string
}

var (
	_ xopbase.Logger     = &TestLogger{}
	_ xopbase.Request    = &Span{}
	_ xopbase.Span       = &Span{}
	_ xopbase.Prefilling = &Prefilling{}
	_ xopbase.Prefilled  = &Prefilled{}
	_ xopbase.Line       = &Line{}
)

func New(t testingT) *TestLogger {
	return &TestLogger{
		t:        t,
		id:       t.Name() + "-" + uuid.New().String(),
		traceMap: make(map[string]*traceInfo),
	}
}

type TestLogger struct {
	lock       sync.Mutex
	t          testingT
	Requests   []*Span
	Spans      []*Span
	Lines      []*Line
	Events     []*Event
	traceCount int
	traceMap   map[string]*traceInfo
	id         string
}

type traceInfo struct {
	spanCount int
	traceNum  int
	spans     map[string]int
}

type Span struct {
	lock          sync.Mutex
	testLogger    *TestLogger
	Trace         trace.Bundle
	IsRequest     bool
	Parent        *Span
	Spans         []*Span
	RequestLines  []*Line
	Lines         []*Line
	short         string
	Metadata      map[string]interface{}
	MetadataTypes map[string]xoputil.BaseAttributeType
	StartTime     time.Time
	EndTime       int64
	Name          string
}

type Prefilling struct {
	Builder
}

type Builder struct {
	Data   map[string]interface{}
	Span   *Span
	kvText []string
}

type Prefilled struct {
	Data   map[string]interface{}
	Span   *Span
	Msg    string
	kvText []string
}

type Line struct {
	Builder
	Level     xopconst.Level
	Timestamp time.Time
	Message   string
	Text      string
	Tmpl      bool
}

type Event struct {
	Type EventType
	Line *Line
	Span *Span
	Msg  string
}

func (t *TestLogger) Log() *xop.Log {
	return xop.NewSeed(xop.WithBase(t)).Request(t.t.Name())
}

// WithLock is provided for thread-safe introspection of the logger
func (l *TestLogger) WithLock(f func(*TestLogger) error) error {
	l.lock.Lock()
	defer l.lock.Unlock()
	return f(l)
}

func (l *TestLogger) CustomEvent(msg string, args ...interface{}) {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.Events = append(l.Events, &Event{
		Type: CustomEvent,
		Msg:  fmt.Sprintf(msg, args...),
	})
}

func (l *TestLogger) ID() string                   { return l.id }
func (l *TestLogger) Close()                       {}
func (l *TestLogger) Buffered() bool               { return false }
func (l *TestLogger) ReferencesKept() bool         { return true }
func (l *TestLogger) SetErrorReporter(func(error)) {}
func (l *TestLogger) Request(ts time.Time, span trace.Bundle, name string) xopbase.Request {
	l.lock.Lock()
	defer l.lock.Unlock()
	s := &Span{
		testLogger:    l,
		IsRequest:     true,
		Trace:         span,
		short:         l.setShort(span, name),
		StartTime:     ts,
		Name:          name,
		Metadata:      make(map[string]interface{}),
		MetadataTypes: make(map[string]xoputil.BaseAttributeType),
	}
	l.Requests = append(l.Requests, s)
	l.Events = append(l.Events, &Event{
		Type: RequestStart,
		Span: s,
	})
	return s
}

// must hold a lock to call setShort
func (l *TestLogger) setShort(span trace.Bundle, name string) string {
	ts := span.Trace.GetTraceID().String()
	if ti, ok := l.traceMap[ts]; ok {
		ti.spanCount++
		ti.spans[span.Trace.GetSpanID().String()] = ti.spanCount
		short := fmt.Sprintf("T%d.%d", ti.traceNum, ti.spanCount)
		l.t.Log("Start span " + short + "=" + span.Trace.HeaderString() + " " + name)
		return short
	}
	l.traceCount++
	l.traceMap[ts] = &traceInfo{
		spanCount: 1,
		traceNum:  l.traceCount,
		spans: map[string]int{
			span.Trace.GetSpanID().String(): 1,
		},
	}
	short := fmt.Sprintf("T%d.%d", l.traceCount, 1)
	l.t.Log("Start span " + short + "=" + span.Trace.HeaderString() + " " + name)
	return short
}

func (s *Span) Done(t time.Time) {
	atomic.StoreInt64(&s.EndTime, t.UnixNano())
	s.testLogger.lock.Lock()
	defer s.testLogger.lock.Unlock()
	if s.IsRequest {
		s.testLogger.Events = append(s.testLogger.Events, &Event{
			Type: RequestDone,
			Span: s,
		})
	} else {
		s.testLogger.Events = append(s.testLogger.Events, &Event{
			Type: SpanDone,
			Span: s,
		})
	}
}

func (s *Span) Flush() {
	s.testLogger.lock.Lock()
	defer s.testLogger.lock.Unlock()
	s.testLogger.Events = append(s.testLogger.Events, &Event{
		Type: FlushEvent,
		Span: s,
	})
}

func (s *Span) Boring(bool)                  {}
func (s *Span) ID() string                   { return s.testLogger.id }
func (s *Span) SetErrorReporter(func(error)) {}

func (s *Span) Span(ts time.Time, span trace.Bundle, name string) xopbase.Span {
	s.testLogger.lock.Lock()
	defer s.testLogger.lock.Unlock()
	s.lock.Lock()
	defer s.lock.Unlock()
	n := &Span{
		testLogger:    s.testLogger,
		Trace:         span,
		short:         s.testLogger.setShort(span, name),
		StartTime:     ts,
		Name:          name,
		Metadata:      make(map[string]interface{}),
		MetadataTypes: make(map[string]xoputil.BaseAttributeType),
	}
	s.Spans = append(s.Spans, n)
	s.testLogger.Spans = append(s.testLogger.Spans, n)
	s.testLogger.Events = append(s.testLogger.Events, &Event{
		Type: SpanStart,
		Span: n,
	})
	return n
}

func (s *Span) NoPrefill() xopbase.Prefilled {
	return &Prefilled{
		Span: s,
	}
}

func (s *Span) StartPrefill() xopbase.Prefilling {
	return &Prefilling{
		Builder: Builder{
			Data: make(map[string]interface{}),
			Span: s,
		},
	}
}

func (p *Prefilling) PrefillComplete(m string) xopbase.Prefilled {
	return &Prefilled{
		Data:   p.Data,
		Span:   p.Span,
		kvText: p.kvText,
		Msg:    m,
	}
}

func (p *Prefilled) Line(level xopconst.Level, t time.Time, _ []uintptr) xopbase.Line {
	atomic.StoreInt64(&p.Span.EndTime, t.UnixNano())
	// TODO: stack traces
	line := &Line{
		Builder: Builder{
			Data: make(map[string]interface{}),
			Span: p.Span,
		},
		Level:     level,
		Timestamp: t,
	}
	for k, v := range p.Data {
		line.Data[k] = v
	}
	if len(p.kvText) != 0 {
		line.kvText = make([]string, len(p.kvText), len(p.kvText)+5)
		copy(line.kvText, p.kvText)
	}
	line.Message = p.Msg
	return line
}

func (l *Line) Static(m string) {
	l.Msg(m)
}

func (l *Line) Msg(m string) {
	l.Message += m
	text := l.Span.short + ": " + l.Message
	if len(l.kvText) > 0 {
		text += " " + strings.Join(l.kvText, " ")
		l.kvText = nil
	}
	l.Text = text
	l.send(text)
}

var templateRE = regexp.MustCompile(`\{.+?\}`)

func (l *Line) Template(m string) {
	l.Tmpl = true
	l.Message += m
	used := make(map[string]struct{})
	text := l.Span.short + ": " +
		templateRE.ReplaceAllStringFunc(l.Message, func(k string) string {
			k = k[1 : len(k)-1]
			if v, ok := l.Data[k]; ok {
				used[k] = struct{}{}
				return fmt.Sprint(v)
			}
			return "''"
		})
	for k, v := range l.Data {
		if _, ok := used[k]; !ok {
			text += " " + k + "=" + fmt.Sprint(v)
		}
	}
	l.Text = text
	l.send(text)
}

func (l Line) send(text string) {
	l.Span.testLogger.t.Log(text)
	l.Span.testLogger.lock.Lock()
	defer l.Span.testLogger.lock.Unlock()
	l.Span.lock.Lock()
	defer l.Span.lock.Unlock()
	l.Span.testLogger.Lines = append(l.Span.testLogger.Lines, &l)
	l.Span.testLogger.Events = append(l.Span.testLogger.Events, &Event{
		Type: LineEvent,
		Line: &l,
	})
	l.Span.Lines = append(l.Span.Lines, &l)
}

func (b *Builder) Any(k string, v interface{}) {
	b.Data[k] = v
	b.kvText = append(b.kvText, fmt.Sprintf("%s=%+v", k, v))
}

func (b *Builder) Enum(k *xopconst.EnumAttribute, v xopconst.Enum) {
	b.Data[k.Key()] = v.String()
	b.kvText = append(b.kvText, fmt.Sprintf("%s=%s(%d)", k.Key(), v.String(), v.Int64()))
}

func (b *Builder) Bool(k string, v bool)              { b.Any(k, v) }
func (b *Builder) Duration(k string, v time.Duration) { b.Any(k, v) }
func (b *Builder) Error(k string, v error)            { b.Any(k, v) }
func (b *Builder) Float64(k string, v float64)        { b.Any(k, v) }
func (b *Builder) Int(k string, v int64)              { b.Any(k, v) }
func (b *Builder) Link(k string, v trace.Trace)       { b.Any(k, v) }
func (b *Builder) String(k string, v string)          { b.Any(k, v) }
func (b *Builder) Time(k string, v time.Time)         { b.Any(k, v) }
func (b *Builder) Uint(k string, v uint64)            { b.Any(k, v) }

func (s *Span) MetadataAny(k *xopconst.AnyAttribute, v interface{}) {
	func() {
		s.testLogger.lock.Lock()
		defer s.testLogger.lock.Unlock()
		s.testLogger.Events = append(s.testLogger.Events, &Event{
			Type: MetadataSet,
			Msg:  k.Key(),
			Span: s,
		})
	}()
	s.lock.Lock()
	defer s.lock.Unlock()
	if k.Multiple() {
		if p, ok := s.Metadata[k.Key()]; ok {
			s.Metadata[k.Key()] = append(p.([]interface{}), v)
		} else {
			s.Metadata[k.Key()] = []interface{}{v}
			s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeAnyArray
		}
	} else {
		s.Metadata[k.Key()] = v
		s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeAny
	}
}

func (s *Span) MetadataBool(k *xopconst.BoolAttribute, v bool) {
	func() {
		s.testLogger.lock.Lock()
		defer s.testLogger.lock.Unlock()
		s.testLogger.Events = append(s.testLogger.Events, &Event{
			Type: MetadataSet,
			Msg:  k.Key(),
			Span: s,
		})
	}()
	s.lock.Lock()
	defer s.lock.Unlock()
	if k.Multiple() {
		if p, ok := s.Metadata[k.Key()]; ok {
			s.Metadata[k.Key()] = append(p.([]interface{}), v)
		} else {
			s.Metadata[k.Key()] = []interface{}{v}
			s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeBoolArray
		}
	} else {
		s.Metadata[k.Key()] = v
		s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeBool
	}
}

func (s *Span) MetadataEnum(k *xopconst.EnumAttribute, v xopconst.Enum) {
	func() {
		s.testLogger.lock.Lock()
		defer s.testLogger.lock.Unlock()
		s.testLogger.Events = append(s.testLogger.Events, &Event{
			Type: MetadataSet,
			Msg:  k.Key(),
			Span: s,
		})
	}()
	s.lock.Lock()
	defer s.lock.Unlock()
	if k.Multiple() {
		if p, ok := s.Metadata[k.Key()]; ok {
			s.Metadata[k.Key()] = append(p.([]interface{}), v)
		} else {
			s.Metadata[k.Key()] = []interface{}{v}
			s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeEnumArray
		}
	} else {
		s.Metadata[k.Key()] = v
		s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeEnum
	}
}

func (s *Span) MetadataFloat64(k *xopconst.Float64Attribute, v float64) {
	func() {
		s.testLogger.lock.Lock()
		defer s.testLogger.lock.Unlock()
		s.testLogger.Events = append(s.testLogger.Events, &Event{
			Type: MetadataSet,
			Msg:  k.Key(),
			Span: s,
		})
	}()
	s.lock.Lock()
	defer s.lock.Unlock()
	if k.Multiple() {
		if p, ok := s.Metadata[k.Key()]; ok {
			s.Metadata[k.Key()] = append(p.([]interface{}), v)
		} else {
			s.Metadata[k.Key()] = []interface{}{v}
			s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeFloat64Array
		}
	} else {
		s.Metadata[k.Key()] = v
		s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeFloat64
	}
}

func (s *Span) MetadataInt64(k *xopconst.Int64Attribute, v int64) {
	func() {
		s.testLogger.lock.Lock()
		defer s.testLogger.lock.Unlock()
		s.testLogger.Events = append(s.testLogger.Events, &Event{
			Type: MetadataSet,
			Msg:  k.Key(),
			Span: s,
		})
	}()
	s.lock.Lock()
	defer s.lock.Unlock()
	if k.Multiple() {
		if p, ok := s.Metadata[k.Key()]; ok {
			s.Metadata[k.Key()] = append(p.([]interface{}), v)
		} else {
			s.Metadata[k.Key()] = []interface{}{v}
			s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeInt64Array
		}
	} else {
		s.Metadata[k.Key()] = v
		s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeInt64
	}
}

func (s *Span) MetadataLink(k *xopconst.LinkAttribute, v trace.Trace) {
	func() {
		s.testLogger.lock.Lock()
		defer s.testLogger.lock.Unlock()
		s.testLogger.Events = append(s.testLogger.Events, &Event{
			Type: MetadataSet,
			Msg:  k.Key(),
			Span: s,
		})
	}()
	s.lock.Lock()
	defer s.lock.Unlock()
	if k.Multiple() {
		if p, ok := s.Metadata[k.Key()]; ok {
			s.Metadata[k.Key()] = append(p.([]interface{}), v)
		} else {
			s.Metadata[k.Key()] = []interface{}{v}
			s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeLinkArray
		}
	} else {
		s.Metadata[k.Key()] = v
		s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeLink
	}
}

func (s *Span) MetadataString(k *xopconst.StringAttribute, v string) {
	func() {
		s.testLogger.lock.Lock()
		defer s.testLogger.lock.Unlock()
		s.testLogger.Events = append(s.testLogger.Events, &Event{
			Type: MetadataSet,
			Msg:  k.Key(),
			Span: s,
		})
	}()
	s.lock.Lock()
	defer s.lock.Unlock()
	if k.Multiple() {
		if p, ok := s.Metadata[k.Key()]; ok {
			s.Metadata[k.Key()] = append(p.([]interface{}), v)
		} else {
			s.Metadata[k.Key()] = []interface{}{v}
			s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeStringArray
		}
	} else {
		s.Metadata[k.Key()] = v
		s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeString
	}
}

func (s *Span) MetadataTime(k *xopconst.TimeAttribute, v time.Time) {
	func() {
		s.testLogger.lock.Lock()
		defer s.testLogger.lock.Unlock()
		s.testLogger.Events = append(s.testLogger.Events, &Event{
			Type: MetadataSet,
			Msg:  k.Key(),
			Span: s,
		})
	}()
	s.lock.Lock()
	defer s.lock.Unlock()
	if k.Multiple() {
		if p, ok := s.Metadata[k.Key()]; ok {
			s.Metadata[k.Key()] = append(p.([]interface{}), v)
		} else {
			s.Metadata[k.Key()] = []interface{}{v}
			s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeTimeArray
		}
	} else {
		s.Metadata[k.Key()] = v
		s.MetadataTypes[k.Key()] = xoputil.BaseAttributeTypeTime
	}
}
