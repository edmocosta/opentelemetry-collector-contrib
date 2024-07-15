// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package localizedtime

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

var longDayNamesStd = []string{
	"Sunday",
	"Monday",
	"Tuesday",
	"Wednesday",
	"Thursday",
	"Friday",
	"Saturday",
}

var shortDayNamesStd = []string{
	"Sun",
	"Mon",
	"Tue",
	"Wed",
	"Thu",
	"Fri",
	"Sat",
}

var shortMonthNamesStd = []string{
	"Jan",
	"Feb",
	"Mar",
	"Apr",
	"May",
	"Jun",
	"Jul",
	"Aug",
	"Sep",
	"Oct",
	"Nov",
	"Dec",
}

var longMonthNamesStd = []string{
	"January",
	"February",
	"March",
	"April",
	"May",
	"June",
	"July",
	"August",
	"September",
	"October",
	"November",
	"December",
}

var dayPeriodsStd = []string{
	"AM",
	"PM",
}

// ErrLayoutMismatch indicates that a provided value does not match its layout counterpart.
var ErrLayoutMismatch = errors.New("value does not match the layout")

// Parse translates the localized time value using the Format method, and parses the
// output to time.Time by using the time.Parse method.
func Parse(layout string, value string, locale Locale) (time.Time, error) {
	pv, err := Format(layout, value, locale)
	if err != nil {
		return time.Time{}, err
	}

	return time.Parse(layout, pv)
}

// ParseInLocation translates the localized time value using the Format method, and parses
// the output to time.Time by using the time.ParseInLocation method.
func ParseInLocation(layout string, value string, locale Locale, location *time.Location) (time.Time, error) {
	pv, err := Format(layout, value, locale)
	if err != nil {
		return time.Time{}, err
	}

	return time.ParseInLocation(layout, pv, location)
}

// Format translates the localized textual time value from the provided locale to English.
// It replaces short and long week days name, months and day periods by the standard
// time.Format representation.
// The first argument must be a native Go time layout. The second argument must be
// parseable using the format string (layout) provided as the first argument, but in the
// original locale language.
func Format(layout string, value string, locale Locale) (string, error) {
	var err error
	var sb strings.Builder
	var layoutOffset, valueOffset int

	sb.Grow(len(layout) + 30)

	for layoutOffset < len(layout) {
		written := false
		var lookupTab, stdTab []string

		switch c := int(layout[layoutOffset]); c {
		case 'J': // January, Jan
			if len(layout) >= layoutOffset+3 && layout[layoutOffset:layoutOffset+3] == "Jan" {
				layoutElem := ""
				if len(layout) >= layoutOffset+7 && layout[layoutOffset:layoutOffset+7] == "January" {
					layoutElem = "January"
					lookupTab = locale.LongMonthNames()
					stdTab = longMonthNamesStd
				} else if !startsWithLowerCase(layout[layoutOffset+3:]) {
					layoutElem = "Jan"
					lookupTab = locale.ShortMonthNames()
					stdTab = shortMonthNamesStd
				}

				if layoutElem == "" {
					break
				}

				if len(lookupTab) == 0 {
					return "", newUnsupportedLayoutElemError(layoutElem, locale)
				}

				layoutOffset += len(layoutElem)
				valueOffset, err = writeLayoutValue(lookupTab, stdTab, valueOffset, value, &sb)
				if err != nil {
					return "", err
				}

				written = true
			}
		case 'M': // Monday, Mon
			if len(layout) >= layoutOffset+3 && layout[layoutOffset:layoutOffset+3] == "Mon" {
				layoutElem := ""
				if len(layout) >= layoutOffset+6 && layout[layoutOffset:layoutOffset+6] == "Monday" {
					layoutElem = "Monday"
					lookupTab = locale.LongDayNames()
					stdTab = longDayNamesStd
				} else if !startsWithLowerCase(layout[layoutOffset+3:]) {
					layoutElem = "Mon"
					lookupTab = locale.ShortDayNames()
					stdTab = shortDayNamesStd
				}

				if layoutElem == "" {
					break
				}

				if len(lookupTab) == 0 {
					return "", newUnsupportedLayoutElemError(layoutElem, locale)
				}

				layoutOffset += len(layoutElem)
				valueOffset, err = writeLayoutValue(lookupTab, stdTab, valueOffset, value, &sb)
				if err != nil {
					return "", err
				}
				written = true
			}
		case 'P', 'p': // PM, pm
			if len(layout) >= layoutOffset+2 && unicode.ToUpper(rune(layout[layoutOffset+1])) == 'M' {
				lookupTab = locale.DayPeriods()
				if len(lookupTab) == 0 {
					return "", newUnsupportedLayoutElemError("PM", locale)
				}

				layoutOffset += 2
				valueOffset, err = writeLayoutValue(lookupTab, dayPeriodsStd, valueOffset, value, &sb)
				if err != nil {
					return "", err
				}
				written = true
			}
		case '_': // _2, _2006, __2
			// Although no translations happens here, it is still necessary to calculate the
			// variable size of `_`  values, so the layoutOffset stays synchronized with
			// its layout element counterpart.
			if len(layout) >= layoutOffset+2 && layout[layoutOffset+1] == '2' {
				var layoutElemSize int
				// _2006 is really a literal _, followed by the long year placeholder
				if len(layout) >= layoutOffset+5 && layout[layoutOffset+1:layoutOffset+5] == "2006" {
					if len(value) >= valueOffset+5 {
						layoutElemSize = 5 // _2006
					}
				} else {
					if len(value) >= valueOffset+2 {
						layoutElemSize = 2 // _2
					}
				}

				if layoutElemSize > 0 {
					layoutOffset += layoutElemSize
					valueOffset, err = writeNextNonSpaceValue(value, valueOffset, layoutElemSize, &sb)
					if err != nil {
						return "", err
					}
					written = true
				}
			}

			if len(layout) >= layoutOffset+3 && layout[layoutOffset+1] == '_' && layout[layoutOffset+2] == '2' {
				if len(value) >= valueOffset+3 {
					layoutOffset += 3
					valueOffset, err = writeNextNonSpaceValue(value, valueOffset, 3, &sb)
					if err != nil {
						return "", err
					}
					written = true
				}
			}
		}

		if !written {
			var writtenSize int
			if len(value) > valueOffset {
				writtenSize, err = sb.WriteRune(rune(value[valueOffset]))
				if err != nil {
					return "", err
				}
			}

			layoutOffset++
			valueOffset += writtenSize
		}
	}

	if len(value) >= valueOffset {
		sb.WriteString(value[valueOffset:])
	}

	return sb.String(), nil
}

func newUnsupportedLayoutElemError(elem string, locale Locale) error {
	return &ErrUnsupportedLayoutElem{
		LayoutElem: elem,
		Language:   locale.Language().String(),
	}
}

func writeNextNonSpaceValue(value string, offset int, max int, sb *strings.Builder) (int, error) {
	nextValOffset, val, err := nextNonSpaceValue(value, offset, max)
	if err != nil {
		return offset, err
	}

	_, err = sb.WriteString(val)
	if err != nil {
		return offset, err
	}

	return nextValOffset, nil
}

func writeLayoutValue(lookupTab, stdTab []string, valueOffset int, value string, sb *strings.Builder) (int, error) {
	newOffset, foundStdValue, val := lookup(lookupTab, valueOffset, value, stdTab)
	if foundStdValue == "" {
		return valueOffset, ErrLayoutMismatch
	}

	_, err := sb.WriteString(foundStdValue)
	if err != nil {
		return valueOffset, err
	}

	newOffset += len(val)
	return newOffset, nil
}

func nextNonSpaceValue(value string, offset int, max int) (newOffset int, val string, err error) {
	newOffset = offset
	for newOffset < len(value) && unicode.IsSpace(rune(value[newOffset])) {
		newOffset++
	}

	if newOffset > len(value) {
		return offset, "", errors.New("next non-space value not found")
	}

	for newOffset < len(value) {
		if !unicode.IsSpace(rune(value[newOffset])) {
			val += string(value[newOffset])
			newOffset++
		} else {
			return newOffset, val, nil
		}

		if len(val) == max {
			return newOffset, val, nil
		}
	}

	return newOffset, val, nil
}

func lookup(lookupTab []string, offset int, val string, stdTab []string) (newOffset int, stdValue string, value string) {
	newOffset = offset
	for newOffset < len(val) && unicode.IsSpace(rune(val[newOffset])) {
		newOffset++
	}

	if newOffset > len(val) {
		return offset, "", val
	}

	for i, v := range lookupTab {
		// Already matched a more specific/longer value
		if stdValue != "" && len(v) <= len(value) {
			continue
		}

		end := newOffset + len(v)
		if end > len(val) {
			continue
		}

		candidate := val[newOffset:end]
		if len(candidate) == len(v) && strings.EqualFold(candidate, v) {
			stdValue = stdTab[i]
			value = candidate
		}
	}

	return newOffset, stdValue, value
}

func startsWithLowerCase(value string) bool {
	if len(value) == 0 {
		return false
	}
	c := value[0]
	return 'a' <= c && c <= 'z'
}

// ErrUnsupportedLayoutElem indicates that a provided layout element is not supported by
// the given locale/language.
type ErrUnsupportedLayoutElem struct {
	LayoutElem string
	Language   string
}

func (u *ErrUnsupportedLayoutElem) Error() string {
	return fmt.Sprintf(`layout element "%s" is not support by the language "%s"`, u.LayoutElem, u.Language)
}

func (u *ErrUnsupportedLayoutElem) Is(err error) bool {
	var target *ErrUnsupportedLayoutElem
	if ok := errors.As(err, &target); ok {
		return u.Language == target.Language && u.LayoutElem == target.LayoutElem
	}
	return false
}
