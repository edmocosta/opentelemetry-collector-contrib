// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package traces // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor/internal/traces"

import (
	"context"
	"reflect"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/multierr"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor/internal/common"
)

type Processor struct {
	contexts []consumer.Traces
	logger   *zap.Logger
}

func NewProcessor(contextStatements []common.ContextStatements, errorMode ottl.ErrorMode, settings component.TelemetrySettings) (*Processor, error) {
	pc, err := common.NewTraceParserCollection(settings, common.WithSpanParser(SpanFunctions()), common.WithSpanEventParser(SpanEventFunctions()), common.WithTraceErrorMode(errorMode))
	if err != nil {
		return nil, err
	}

	contexts := make([]consumer.Traces, len(contextStatements))
	var errors error
	for i, cs := range contextStatements {
		context, err := pc.ParseContextStatements(cs)
		if err != nil {
			errors = multierr.Append(errors, err)
		}
		contexts[i] = context
	}

	if errors != nil {
		return nil, errors
	}

	return &Processor{
		contexts: contexts,
		logger:   settings.Logger,
	}, nil
}

func (p *Processor) ProcessTraces(ctx context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	contextCache := make(map[string]*pcommon.Map, len(p.contexts))
	for _, c := range p.contexts {
		cacheKey := reflect.TypeOf(c).String()
		cache, ok := contextCache[cacheKey]
		if !ok {
			m := pcommon.NewMap()
			cache = &m
			contextCache[cacheKey] = cache
		}
		err := c.ConsumeTraces(common.WithCache(ctx, cache), td)
		if err != nil {
			p.logger.Error("failed processing traces", zap.Error(err))
			return td, err
		}
	}
	return td, nil
}
