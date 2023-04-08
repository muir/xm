// This file is generated, DO NOT EDIT.  It comes from the corresponding .zzzgo file

/*
Package xoprecorder provides an introspective xopbase.Logger. All logging
is saved to memory and can be examined. Memory is only freed when the logger
is cleaned up with garbage collection.
*/
package xoprecorder

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"sync"
	"time"

	"github.com/xoplog/xop-go/xopat"
	"github.com/xoplog/xop-go/xopbase"
	"github.com/xoplog/xop-go/xopbase/xopbaseutil"
	"github.com/xoplog/xop-go/xopnum"
	"github.com/xoplog/xop-go/xoptrace"
	"github.com/xoplog/xop-go/xoputil"

	"github.com/google/uuid"
	"github.com/muir/list"
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

var (
	_ xopbase.Logger     = &Logger{}
	_ xopbase.Request    = &Span{}
	_ xopbase.Span       = &Span{}
	_ xopbase.Prefilling = &Prefilling{}
	_ xopbase.Prefilled  = &Prefilled{}
	_ xopbase.Line       = &Line{}
)

func New() *Logger {
	return &Logger{
		id:       "xoprecorder-" + uuid.New().String(),
		traceMap: make(map[string]*traceInfo),
	}
}

type Logger struct {
	lock       sync.Mutex
	Requests   []*Span
	Spans      []*Span
	Lines      []*Line
	Events     []*Event
	traceCount int
	traceMap   map[string]*traceInfo
	id         string
	linePrefix string
}

type traceInfo struct {
	requestCount int
	traceNum     int
	spans        map[string]*Span
}

type Span struct {
	EndTime            int64
	provisionalEndTime int64
	lock               sync.Mutex
	logger             *Logger
	RequestNum         int // sequence of requests with the same traceID
	Bundle             xoptrace.Bundle
	IsRequest          bool
	Parent             *Span
	Spans              []*Span
	Lines              []*Line
	StartTime          time.Time
	Name               string
	SequenceCode       string
	Ctx                context.Context
	SourceInfo         *xopbase.SourceInfo
	SpanMetadata       xopbaseutil.SpanMetadata
}

type Prefilling struct {
	Builder
}

type Builder struct {
	Enums    map[string]*xopat.EnumAttribute
	Data     map[string]interface{}
	DataType map[string]xopbase.DataType
	Span     *Span
}

type Prefilled struct {
	Enums    map[string]*xopat.EnumAttribute
	Data     map[string]interface{}
	DataType map[string]xopbase.DataType
	Span     *Span
	Msg      string
}

type Line struct {
	Builder
	Level     xopnum.Level
	Timestamp time.Time
	Message   string // Prefill text + line text (template not evaluated)
	Tmpl      string // un-evaluated template
	AsLink    *xoptrace.Trace
	AsModel   *xopbase.ModelArg
	Stack     []runtime.Frame
}

type Event struct {
	Type      EventType
	Line      *Line
	Span      *Span
	Msg       string
	Attribute xopat.AttributeInterface
	Done      bool
	Value     interface{}
}

// WithLock is provided for thread-safe introspection of the logger
func (log *Logger) WithLock(f func(*Logger) error) error {
	log.lock.Lock()
	defer log.lock.Unlock()
	return f(log)
}

func (log *Logger) CustomEvent(msg string, args ...interface{}) {
	log.lock.Lock()
	defer log.lock.Unlock()
	log.Events = append(log.Events, &Event{
		Type: CustomEvent,
		Msg:  fmt.Sprintf(msg, args...),
	})
}

// ID is a required method for xopbase.Logger
func (log *Logger) ID() string { return log.id }

// Buffered is a required method for xopbase.Logger
func (log *Logger) Buffered() bool { return false }

// ReferencesKept is a required method for xopbase.Logger
func (log *Logger) ReferencesKept() bool { return true }

// SetErrorReporter is a required method for xopbase.Logger
func (log *Logger) SetErrorReporter(func(error)) {}

// Request is a required method for xopbase.Logger
func (log *Logger) Request(ctx context.Context, ts time.Time, bundle xoptrace.Bundle, name string, sourceInfo xopbase.SourceInfo) xopbase.Request {
	log.lock.Lock()
	defer log.lock.Unlock()
	s := &Span{
		logger:     log,
		IsRequest:  true,
		Bundle:     bundle,
		StartTime:  ts,
		Name:       name,
		Ctx:        ctx,
		SourceInfo: &sourceInfo,
	}
	s.setRequestNumber()
	log.Requests = append(log.Requests, s)
	log.Events = append(log.Events, &Event{
		Type: RequestStart,
		Span: s,
	})
	return s
}

// must hold a lock to call setRequestNumber
func (span *Span) setRequestNumber() {
	ts := span.Bundle.Trace.GetTraceID().String()
	if ti, ok := span.logger.traceMap[ts]; ok {
		ti.requestCount++
		span.RequestNum = ti.requestCount
		ti.spans[span.Bundle.Trace.SpanID().String()] = span
		return
	}
	span.logger.traceCount++
	span.RequestNum = 1
	span.logger.traceMap[ts] = &traceInfo{
		requestCount: 1,
		traceNum:     span.logger.traceCount,
		spans: map[string]*Span{
			span.Bundle.Trace.SpanID().String(): span,
		},
	}
}

// must hold a lock to call setSpanNumber
func (span *Span) setSpanNumber() {
	ts := span.Bundle.Trace.GetTraceID().String()
	ti := span.logger.traceMap[ts]
	span.RequestNum = span.Parent.RequestNum
	ti.spans[span.Bundle.Trace.SpanID().String()] = span
}

// Done is a required method for xopbase.Span
func (span *Span) Done(t time.Time, final bool) {
	xoputil.AtomicMaxInt64(&span.EndTime, xoputil.AtomicMaxInt64(&span.provisionalEndTime, t.UnixNano()))
	span.logger.lock.Lock()
	defer span.logger.lock.Unlock()
	if span.IsRequest {
		span.logger.Events = append(span.logger.Events, &Event{
			Type: RequestDone,
			Span: span,
			Done: final,
		})
	} else {
		span.logger.Events = append(span.logger.Events, &Event{
			Type: SpanDone,
			Span: span,
			Done: final,
		})
	}
}

// Done is a required method for xopbase.Request
func (span *Span) Flush() {
	span.logger.lock.Lock()
	defer span.logger.lock.Unlock()
	span.logger.Events = append(span.logger.Events, &Event{
		Type: FlushEvent,
		Span: span,
	})
}

// Final is a required method for xopbase.Request
func (span *Span) Final() {}

// Boring is a required method for xopbase.Span
func (span *Span) Boring(bool) {}

// ID is a required method for xopbase.Span
func (span *Span) ID() string { return span.logger.id }

// ID is a required method for xopbase.Request
func (span *Span) SetErrorReporter(func(error)) {}

// Span is a required method for xopbase.Span
func (span *Span) Span(ctx context.Context, ts time.Time, bundle xoptrace.Bundle, name string, spanSequenceCode string) xopbase.Span {
	n := &Span{
		logger:       span.logger,
		Bundle:       bundle,
		StartTime:    ts,
		Name:         name,
		SequenceCode: spanSequenceCode,
		Ctx:          ctx,
		Parent:       span,
	}
	event := &Event{
		Type: SpanStart,
		Span: n,
	}
	span.logger.lock.Lock()
	defer span.logger.lock.Unlock()
	span.lock.Lock()
	defer span.lock.Unlock()
	n.setSpanNumber()
	span.Spans = append(span.Spans, n)
	span.logger.Spans = append(span.logger.Spans, n)
	span.logger.Events = append(span.logger.Events, event)
	return n
}

// ParentRequest returns the span that is the request-level parent
// of the current span. If the current span is a request, it returns
// the current span.
func (span *Span) ParentRequest() *Span {
	for {
		if span.IsRequest {
			return span
		}
		span = span.Parent
	}
}

// NoPrefill is a required method for xopbase.Span
func (span *Span) NoPrefill() xopbase.Prefilled {
	return &Prefilled{
		Span: span,
	}
}

// StartPrefill is a required method for xopbase.Span
func (span *Span) StartPrefill() xopbase.Prefilling {
	return &Prefilling{
		Builder: Builder{
			Enums:    make(map[string]*xopat.EnumAttribute),
			Data:     make(map[string]interface{}),
			DataType: make(map[string]xopbase.DataType),
			Span:     span,
		},
	}
}

// PrefillComplete is a required method for xopbase.Prefilling
func (p *Prefilling) PrefillComplete(m string) xopbase.Prefilled {
	return &Prefilled{
		Enums:    p.Enums,
		Data:     p.Data,
		DataType: p.DataType,
		Span:     p.Span,
		Msg:      m,
	}
}

// Line is a required method for xopbase.Prefilled
func (p *Prefilled) Line(level xopnum.Level, t time.Time, frames []runtime.Frame) xopbase.Line {
	xoputil.AtomicMaxInt64(&p.Span.provisionalEndTime, t.UnixNano())
	line := &Line{
		Builder: Builder{
			Enums:    make(map[string]*xopat.EnumAttribute),
			Data:     make(map[string]interface{}),
			DataType: make(map[string]xopbase.DataType),
			Span:     p.Span,
		},
		Level:     level,
		Timestamp: t,
		Stack:     list.Copy(frames),
	}
	for k, v := range p.Data {
		line.Data[k] = v
		line.DataType[k] = p.DataType[k]
		if e, ok := p.Enums[k]; ok {
			line.Enums[k] = e
		}
	}
	line.Message = p.Msg
	return line
}

// Link is a required method for xopbase.Line
func (line *Line) Link(m string, v xoptrace.Trace) {
	line.AsLink = &v
	line.Message += m
	line.send()
}

// Model is a required method for xopbase.Line
func (line *Line) Model(m string, v xopbase.ModelArg) {
	line.AsModel = &v
	line.Message += m
	line.send()
}

// Msg is a required method for xopbase.Line
func (line *Line) Msg(m string) {
	line.Message += m
	line.send()
}

var templateRE = regexp.MustCompile(`\{.+?\}`)

// Template is a required method for xopbase.Line
func (line *Line) Template(m string) {
	line.Tmpl = line.Message + m
	used := make(map[string]struct{})
	msg := templateRE.ReplaceAllStringFunc(line.Tmpl, func(k string) string {
		k = k[1 : len(k)-1]
		if v, ok := line.Data[k]; ok {
			used[k] = struct{}{}
			return fmt.Sprint(v)
		}
		return "''"
	})
	line.Message = msg
	line.send()
}

func (line Line) send() {
	line.Span.logger.lock.Lock()
	defer line.Span.logger.lock.Unlock()
	line.Span.lock.Lock()
	defer line.Span.lock.Unlock()
	line.Span.logger.Lines = append(line.Span.logger.Lines, &line)
	line.Span.logger.Events = append(line.Span.logger.Events, &Event{
		Type: LineEvent,
		Line: &line,
	})
	line.Span.Lines = append(line.Span.Lines, &line)
}

func (line *Line) Text() string {
	var start string
	var end string
	msg := line.Message
	used := make(map[string]struct{})
	switch {
	case line.AsLink != nil:
		start = "LINK:"
		end = line.AsLink.String()
	case line.AsModel != nil:
		line.AsModel.Encode()
		start = "MODEL:"
		end = string(line.AsModel.Encoded)
	case line.Tmpl != "":
		used := make(map[string]struct{})
		msg = templateRE.ReplaceAllStringFunc(line.Tmpl, func(k string) string {
			k = k[1 : len(k)-1]
			if v, ok := line.Data[k]; ok {
				used[k] = struct{}{}
				return fmt.Sprint(v)
			}
			return "''"
		})
	default:
		end = line.Message
	}
	// XXX include short
	text := start + msg
	for k, v := range line.Data {
		if _, ok := used[k]; !ok {
			text += " " + k + "=" + fmt.Sprint(v)
		}
	}
	if end != "" {
		text += " " + end
	}
	return text
}

// TemplateOrMessage returns the line template (if set) or the template
// message (Msg) if there is no template
func (line *Line) TemplateOrMessage() string {
	if line.Tmpl != "" {
		return line.Tmpl
	}
	return line.Message
}

func (b *Builder) any(k string, v interface{}, dt xopbase.DataType) {
	b.Data[k] = v
	b.DataType[k] = dt
}

// Enum is a required method for xopbase.ObjectParts
func (b *Builder) Enum(k *xopat.EnumAttribute, v xopat.Enum) {
	ks := k.Key()
	b.Enums[ks] = k
	b.Data[ks] = v
	b.DataType[ks] = xopbase.EnumDataType
}

// Any is a required method for xopbase.ObjectParts
func (b *Builder) Any(k string, v xopbase.ModelArg) { b.any(k, v, xopbase.AnyDataType) }

// Bool is a required method for xopbase.ObjectParts
func (b *Builder) Bool(k string, v bool) { b.any(k, v, xopbase.BoolDataType) }

// Duration is a required method for xopbase.ObjectParts
func (b *Builder) Duration(k string, v time.Duration) { b.any(k, v, xopbase.DurationDataType) }

// Time is a required method for xopbase.ObjectParts
func (b *Builder) Time(k string, v time.Time) { b.any(k, v, xopbase.TimeDataType) }

// Float64 is a required method for xopbase.ObjectParts
func (b *Builder) Float64(k string, v float64, dt xopbase.DataType) { b.any(k, v, dt) }

// Int64 is a required method for xopbase.ObjectParts
func (b *Builder) Int64(k string, v int64, dt xopbase.DataType) { b.any(k, v, dt) }

// String is a required method for xopbase.ObjectParts
func (b *Builder) String(k string, v string, dt xopbase.DataType) { b.any(k, v, dt) }

// Uint64 is a required method for xopbase.ObjectParts
func (b *Builder) Uint64(k string, v uint64, dt xopbase.DataType) { b.any(k, v, dt) }

// MetadataAny is a required method for xopbase.Span
func (s *Span) MetadataAny(k *xopat.AnyAttribute, v xopbase.ModelArg) {
	s.SpanMetadata.MetadataAny(k, v)
	s.logger.lock.Lock()
	defer s.logger.lock.Unlock()
	s.logger.Events = append(s.logger.Events, &Event{
		Type:      MetadataSet,
		Attribute: k,
		Span:      s,
		Value:     v,
	})
}

// MetadataBool is a required method for xopbase.Span
func (s *Span) MetadataBool(k *xopat.BoolAttribute, v bool) {
	s.SpanMetadata.MetadataBool(k, v)
	s.logger.lock.Lock()
	defer s.logger.lock.Unlock()
	s.logger.Events = append(s.logger.Events, &Event{
		Type:      MetadataSet,
		Attribute: k,
		Span:      s,
		Value:     v,
	})
}

// MetadataEnum is a required method for xopbase.Span
func (s *Span) MetadataEnum(k *xopat.EnumAttribute, v xopat.Enum) {
	s.SpanMetadata.MetadataEnum(k, v)
	s.logger.lock.Lock()
	defer s.logger.lock.Unlock()
	s.logger.Events = append(s.logger.Events, &Event{
		Type:      MetadataSet,
		Attribute: k,
		Span:      s,
		Value:     v,
	})
}

// MetadataFloat64 is a required method for xopbase.Span
func (s *Span) MetadataFloat64(k *xopat.Float64Attribute, v float64) {
	s.SpanMetadata.MetadataFloat64(k, v)
	s.logger.lock.Lock()
	defer s.logger.lock.Unlock()
	s.logger.Events = append(s.logger.Events, &Event{
		Type:      MetadataSet,
		Attribute: k,
		Span:      s,
		Value:     v,
	})
}

// MetadataInt64 is a required method for xopbase.Span
func (s *Span) MetadataInt64(k *xopat.Int64Attribute, v int64) {
	s.SpanMetadata.MetadataInt64(k, v)
	s.logger.lock.Lock()
	defer s.logger.lock.Unlock()
	s.logger.Events = append(s.logger.Events, &Event{
		Type:      MetadataSet,
		Attribute: k,
		Span:      s,
		Value:     v,
	})
}

// MetadataLink is a required method for xopbase.Span
func (s *Span) MetadataLink(k *xopat.LinkAttribute, v xoptrace.Trace) {
	s.SpanMetadata.MetadataLink(k, v)
	s.logger.lock.Lock()
	defer s.logger.lock.Unlock()
	s.logger.Events = append(s.logger.Events, &Event{
		Type:      MetadataSet,
		Attribute: k,
		Span:      s,
		Value:     v,
	})
}

// MetadataString is a required method for xopbase.Span
func (s *Span) MetadataString(k *xopat.StringAttribute, v string) {
	s.SpanMetadata.MetadataString(k, v)
	s.logger.lock.Lock()
	defer s.logger.lock.Unlock()
	s.logger.Events = append(s.logger.Events, &Event{
		Type:      MetadataSet,
		Attribute: k,
		Span:      s,
		Value:     v,
	})
}

// MetadataTime is a required method for xopbase.Span
func (s *Span) MetadataTime(k *xopat.TimeAttribute, v time.Time) {
	s.SpanMetadata.MetadataTime(k, v)
	s.logger.lock.Lock()
	defer s.logger.lock.Unlock()
	s.logger.Events = append(s.logger.Events, &Event{
		Type:      MetadataSet,
		Attribute: k,
		Span:      s,
		Value:     v,
	})
}
