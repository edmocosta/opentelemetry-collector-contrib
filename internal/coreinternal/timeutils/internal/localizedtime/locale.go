// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
//go:generate go run generator.go

package localizedtime

import (
	"fmt"

	"golang.org/x/text/language"
)

type Locale interface {
	Language() *language.Tag

	LongDayNames() []string

	ShortDayNames() []string

	LongMonthNames() []string

	ShortMonthNames() []string

	DayPeriods() []string
}

type genericLocale struct {
	lang  *language.Tag
	table [5][]string
}

func (g *genericLocale) LongDayNames() []string {
	return g.table[longDayNamesField]
}

func (g *genericLocale) ShortDayNames() []string {
	return g.table[shortDayNamesField]
}

func (g *genericLocale) LongMonthNames() []string {
	return g.table[longMonthNamesField]
}

func (g *genericLocale) ShortMonthNames() []string {
	return g.table[shortMonthNamesField]
}

func (g *genericLocale) DayPeriods() []string {
	return g.table[dayPeriodsField]
}

func (g *genericLocale) Language() *language.Tag {
	return g.lang
}

// ErrUnsupportedLocale indicates that a provided language.Tag is not supported by the
// default CLDR generic locales.
type ErrUnsupportedLocale struct {
	lang *language.Tag
}

func (e *ErrUnsupportedLocale) Error() string {
	return fmt.Sprintf("locale %s not supported", e.lang.String())
}

// NewLocale creates a new generic locale based on the CLDR gregorian calendar translations.
func NewLocale(lang *language.Tag) (Locale, error) {
	table, ok := tables[lang.String()]
	if !ok {
		return nil, &ErrUnsupportedLocale{lang}
	}

	locale := genericLocale{lang: lang, table: table}
	return &locale, nil
}
