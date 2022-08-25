package xopjson

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/muir/xop-go/trace"
	"github.com/muir/xop-go/xopbase"
	"github.com/muir/xop-go/xopbytes"
	"github.com/muir/xop-go/xopconst"
	"github.com/muir/xop-go/xoputil"

	"github.com/google/uuid"
)

var _ xopbase.Logger = &Logger{}
var _ xopbase.Request = &request{}
var _ xopbase.Span = &span{}
var _ xopbase.Line = &line{}
var _ xopbase.Prefilling = &prefilling{}
var _ xopbase.Prefilled = &prefilled{}

type Option func(*Logger, *xoputil.Prealloc)

type timeOption int

const (
	epochTime timeOption = iota
	epochQuoted
	strftimeTime
	timeTimeFormat
)

type Logger struct {
	writer                xopbytes.BytesWriter
	withGoroutine         bool
	fastKeys              bool
	durationFormat        DurationOption
	timeOption            timeOption
	timeFormat            string
	timeDivisor           time.Duration
	spanStarts            bool
	spanChangesOnly       bool
	id                    uuid.UUID
	tagOption             TagOption
	requestCount          int64 // only incremented with tagOption == TraceSequenceNumberTagOption
	perRequestBufferLimit int
	attributesObject      bool
	closeRequest          chan struct{}
	builderPool           sync.Pool // filled with *builder
	linePool              sync.Pool // filled with *line
	preallocatedKeys      [100]byte
	durationKey           []byte
	// TODO: prefilledPool	sync.Pool
	// TODO: timeKey []byte
	// TODO: timestampKey          []byte
}

type request struct {
	span
	errorFunc         func(error)
	writeBuffer       []byte
	completedLines    chan *line
	flushRequest      chan struct{}
	flushComplete     chan struct{}
	completedBuilders chan *builder
	idNum             int64
}

type span struct {
	endTime            int64
	writer             xopbytes.BytesRequest
	trace              trace.Bundle
	logger             *Logger
	name               string
	request            *request
	startTime          time.Time
	serializationCount int32
	attributes         AttributeBuilder
	sequenceCode       string
	spanIDBuffer       [len(`"trace.header":`) + 55 + 2]byte
	spanIDPrebuilt     xoputil.JBuilder
}

type prefilling struct {
	*builder
}

type prefilled struct {
	data          []byte
	preEncodedMsg []byte
	span          *span
}

type line struct {
	*builder
	level                xopconst.Level
	timestamp            time.Time
	prefillMsgPreEncoded []byte
}

type builder struct {
	xoputil.JBuilder
	encoder           *json.Encoder
	span              *span
	attributesStarted bool
	attributesWanted  bool
}

type DurationOption int

const (
	AsNanos   DurationOption = iota // int64(duration)
	AsMicros                        // int64(duration / time.Milliscond)
	AsMillis                        // int64(duration / time.Milliscond)
	AsSeconds                       // int64(duration / time.Second)
	AsString                        // duration.String()
)

// WithDuration specifies the format used for durations. If
// set, durations will be recorded for spans and requests.
func WithDuration(key string, durationFormat DurationOption) Option {
	return func(l *Logger, p *xoputil.Prealloc) {
		l.durationKey = p.Pack(xoputil.BuildKey(key))
		l.durationFormat = durationFormat
	}
}

type TagOption int

const (
	SpanIDTagOption       TagOption = 1 << iota // 16 bytes hex
	TraceIDTagOption                = 1 << iota // 32 bytes hex
	TraceHeaderTagOption            = 1 << iota // 2+1+32+1+16+1+2 = 55 bytes/
	TraceNumberTagOption            = 1 << iota // integer trace count
	SpanSequenceTagOption           = 1 << iota // eg ".1.A"
)

// WithSpanTags specifies how lines should reference the span that they're within.
// The default is SpanSequenceTagOption if WithBufferedLines(true) is used
// because in that sitatuion, there are other clues that can be used to
// figure out the spanID and traceID.  WithSpanTags() also modifies how spans
// (but not requests) are logged: both TraceNumberTagOption, TraceNumberTagOption
// apply to spans also.
//
// SpanIDTagOption indicates the the spanID should be included.  The key
// is "span.id".
//
// TraceIDTagOption indicates the traceID should be included.  If
// TagLinesWithSpanSequence(true) was used, then the span can be derrived
// that way.  The key is "trace.id".
//
// TraceNumberTagOption indicates that that a trace sequence
// number should be included in each line.  This also means that each
// Request will emit a small record tying the traceID to a squence number.
// The key is "trace.num".
//
// SpanSequenceTagOption indicates that the dot-notation span context
// string should be included in each line.  The key is "span.ctx".

func WithSpanTags(tagOption TagOption) Option {
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.tagOption = tagOption
	}
}

// WithSpanStarts controls logging of the start of spans and requests.
// When false, span-level data is output only when when Done() is called.
// Done() can be called more than once.
func WithSpanStarts(b bool) Option {
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.spanStarts = b
	}
}

// WithSpanChangesOnly controls the data included when span-level and
// request-level data is logged.  When true, only changed fields will
// be output. When false, all data will be output at each call to Done().
func WithSpanChangesOnly(b bool) Option {
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.spanChangesOnly = b
	}
}

// WithBufferedLines indciates if line data should be buffered until
// Flush() is called.  If not, lines are emitted as they're completed.
// A value of zero (the default) indicates that lines are not buffered.
//
// A value less than 1024 will panic.  1MB is the suggested value.
func WithBufferedLines(bufferSize int) Option {
	if bufferSize < 1024 {
		panic("bufferSize too small")
	}
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.perRequestBufferLimit = bufferSize
	}
}

func WithUncheckedKeys(b bool) Option {
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.fastKeys = b
	}
}

// WithAttributesObject specifies if the user-defined
// attributes on lines, spans, and requests should be
// inside an "attributes" sub-object or part of the main
// object.
func WithAttributesObject(b bool) Option {
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.attributesObject = b
	}
}

// TODO: allow custom error formats

// WithStrftime specifies how to format timestamps.
// See // https://github.com/phuslu/fasttime for the supported
// formats.
func WithStrftime(format string) Option {
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.timeOption = strftimeTime
		l.timeFormat = format
	}
}

// WithTimeFormat specifies the use of the "time" package's
// Time.Format for formatting times.
func WithTimeFormat(format string) Option {
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.timeOption = timeTimeFormat
		l.timeFormat = format
	}
}

// WithEpochTime specifies that time values are formatted as an
// integer time since Jan 1 1970.  If the units is seconds, then
// it is seconds since Jan 1 1970.  If the units is nanoseconds,
// then it is nanoseconds since Jan 1 1970.  Etc.
//
// Note that the JSON specification specifies int's are 32 bits,
// not 64 bits so a compliant JSON parser could fail for seconds
// since 1970 starting in year 2038.  For microseconds, and
// nanoseconds, a complicant parser alerady fails.
func WithEpochTime(units time.Duration) Option {
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.timeOption = epochTime
		l.timeDivisor = units
	}
}

// WithQuotedEpochTime specifies that time values are formatted an
// integer string (integer with quotes around it) representing time
// since Jan 1 1970.  If the units is seconds, then
// it is seconds since Jan 1 1970.  If the units is nanoseconds,
// then it is nanoseconds since Jan 1 1970.  Etc.
//
// Note most JSON parsers can parse into an integer if given a
// a quoted integer.
func WithQuotedEpochTime(units time.Duration) Option {
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.timeOption = epochQuoted
		l.timeDivisor = units
	}
}

// TODO
func WithGoroutineID(b bool) Option {
	return func(l *Logger, _ *xoputil.Prealloc) {
		l.withGoroutine = b
	}
}
