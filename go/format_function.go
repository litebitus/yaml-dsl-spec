package yamldslspec

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	formatPartLimit       = 1000
	maximumTimeDigits     = 9
	earliestFormatSeconds = -62167219200
	latestFormatSeconds   = 253402300800
	secondsPerDay         = 86400
	textOperandKinds      = "text"
	numberOperandKinds    = "a number"
	wholeOperandKinds     = "a whole number"
)

type formatVerbRule struct {
	acceptedKinds    string
	takesOperand     bool
	acceptsText      bool
	acceptsWhole     bool
	acceptsNumber    bool
	acceptsBoolean   bool
	readsWidth       bool
	readsPrecision   bool
	readsZeroPadding bool
	readsPlusSign    bool
	readsDateFormat  bool
}

var formatVerbRules = map[rune]formatVerbRule{
	'%': {},
	's': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'q': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'u': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'l': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'A': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'B': {acceptedKinds: textOperandKinds, takesOperand: true, acceptsText: true, readsWidth: true, readsPrecision: true},
	'v': {
		acceptedKinds: "text, a whole number or a boolean", takesOperand: true,
		acceptsText: true, acceptsWhole: true, acceptsBoolean: true, readsWidth: true,
	},
	'd': {
		acceptedKinds: wholeOperandKinds, takesOperand: true,
		acceptsWhole: true, readsWidth: true, readsZeroPadding: true, readsPlusSign: true,
	},
	'x': {
		acceptedKinds: "a whole number or text", takesOperand: true,
		acceptsText: true, acceptsWhole: true, readsWidth: true, readsZeroPadding: true,
	},
	'X': {
		acceptedKinds: "a whole number or text", takesOperand: true,
		acceptsText: true, acceptsWhole: true, readsWidth: true, readsZeroPadding: true,
	},
	'f': {
		acceptedKinds: numberOperandKinds, takesOperand: true, acceptsWhole: true, acceptsNumber: true,
		readsWidth: true, readsPrecision: true, readsZeroPadding: true, readsPlusSign: true,
	},
	'C': {
		acceptedKinds: numberOperandKinds, takesOperand: true, acceptsWhole: true, acceptsNumber: true,
		readsWidth: true, readsPrecision: true, readsDateFormat: true,
	},
}

const dateVerbLetters = "YymdHMSLz"

// LayoutPart is one part of a layout: its literal text, or a verb.
type LayoutPart struct {
	Text string
	Verb *LayoutVerb
}

// LayoutVerb is a verb as a layout writes it: its letter, its flags, its width
// and precision, and its date format, nil where left out.
type LayoutVerb struct {
	Letter      rune
	LeftAligned bool
	PlusSigned  bool
	ZeroPadded  bool
	Width       *int
	Precision   *int
	DateFormat  *DateFormat
}

// DateFormat is a date format: its parts, and its time offset as written, nil
// where left out.
type DateFormat struct {
	Parts  []DateFormatPart
	Offset *string
}

// DateFormatPart is one part of a date format: its literal text, `%%` read as
// text, or a date verb's letter, with its flags and width.
type DateFormatPart struct {
	Text        string
	DateVerb    rune
	LeftAligned bool
	ZeroPadded  bool
	Width       *int
}

type formatDirective struct {
	LayoutVerb
	writtenText       string
	overPartLimit     bool
	dateFormatProblem error
}

type formatOperandKind int

const (
	textOperand formatOperandKind = iota
	wholeOperand
	numberOperand
	booleanOperand
	otherOperand
)

type formatOperand struct {
	kind         formatOperandKind
	kindName     string
	textValue    string
	wholeValue   int64
	numberValue  float64
	booleanValue bool
}

// FormatLayout is the standard's format function: the layout with each verb
// written from the next operand, or the problem that stops it.
func FormatLayout(layout string, operands []any) (string, error) {
	layoutParts, layoutError := ParseLayout(layout)
	if layoutError != nil {
		return "", layoutError
	}
	var writtenText strings.Builder
	nextOperand := 0
	for _, layoutPart := range layoutParts {
		if layoutPart.Verb == nil {
			writtenText.WriteString(layoutPart.Text)
			continue
		}
		if nextOperand >= len(operands) {
			return "", fmt.Errorf("layout %s has more verbs than operands", CodeSpan(layout))
		}
		layoutVerb := *layoutPart.Verb
		verbRule := formatVerbRules[layoutVerb.Letter]
		operand := formatOperandOf(operands[nextOperand])
		nextOperand++
		verbText, verbError := formattedVerbText(layoutVerb, verbRule, operand, nextOperand)
		if verbError != nil {
			return "", verbError
		}
		writtenText.WriteString(paddedVerbText(layoutVerb, verbRule, verbText))
	}
	if nextOperand < len(operands) {
		return "", fmt.Errorf("layout %s has more operands than verbs", CodeSpan(layout))
	}
	return writtenText.String(), nil
}

// ParseLayout reads a layout into its parts, literal text and verbs, `%%`
// read as text; else the first problem in the layout.
func ParseLayout(layout string) ([]LayoutPart, error) {
	layoutParts := []LayoutPart{}
	var literalText strings.Builder
	layoutRunes := []rune(layout)
	for position := 0; position < len(layoutRunes); position++ {
		if layoutRunes[position] != '%' {
			literalText.WriteRune(layoutRunes[position])
			continue
		}
		directive, endPosition, isComplete := readFormatDirective(layoutRunes, position)
		if directive.dateFormatProblem != nil {
			return nil, directive.dateFormatProblem
		}
		if !isComplete {
			return nil, fmt.Errorf("layout %s ends inside a verb", CodeSpan(layout))
		}
		position = endPosition
		verbRule, isVerb := formatVerbRules[directive.Letter]
		if !isVerb {
			return nil, fmt.Errorf("%s is not a verb `format` writes", CodeSpan(directive.writtenText))
		}
		if directiveError := directivePartProblem(directive, verbRule); directiveError != nil {
			return nil, directiveError
		}
		if !verbRule.takesOperand {
			literalText.WriteRune('%')
			continue
		}
		if literalText.Len() > 0 {
			layoutParts = append(layoutParts, LayoutPart{Text: literalText.String()})
			literalText.Reset()
		}
		layoutVerb := directive.LayoutVerb
		layoutParts = append(layoutParts, LayoutPart{Verb: &layoutVerb})
	}
	if literalText.Len() > 0 {
		layoutParts = append(layoutParts, LayoutPart{Text: literalText.String()})
	}
	return layoutParts, nil
}

func readFormatDirective(layoutRunes []rune, startPosition int) (formatDirective, int, bool) {
	directive := formatDirective{}
	position := readFlagsAndWidth(layoutRunes, startPosition+1, &directive)
	if position < len(layoutRunes) && layoutRunes[position] == '{' {
		var isClosed bool
		position, isClosed = readDateFormat(layoutRunes, position+1, &directive)
		if !isClosed {
			return directive, position, false
		}
	}
	position = readPrecision(layoutRunes, position, &directive)
	if position >= len(layoutRunes) {
		return directive, position, false
	}
	directive.Letter = layoutRunes[position]
	directive.writtenText = string(layoutRunes[startPosition : position+1])
	return directive, position, true
}

// readFlagsAndWidth reads a verb's or a date verb's flags and width, answering where they end.
func readFlagsAndWidth(layoutRunes []rune, startPosition int, directive *formatDirective) int {
	position := startPosition
	for position < len(layoutRunes) && strings.ContainsRune("-+0", layoutRunes[position]) {
		switch layoutRunes[position] {
		case '-':
			directive.LeftAligned = true
		case '+':
			directive.PlusSigned = true
		default:
			directive.ZeroPadded = true
		}
		position++
	}
	directive.Width, position = readFormatNumber(layoutRunes, position, directive)
	return position
}

// readPrecision reads a precision where one is written, `.` alone being 0, answering where it ends.
func readPrecision(layoutRunes []rune, position int, directive *formatDirective) int {
	if position >= len(layoutRunes) || layoutRunes[position] != '.' {
		return position
	}
	directive.Precision, position = readFormatNumber(layoutRunes, position+1, directive)
	if directive.Precision == nil {
		noPrecision := 0
		directive.Precision = &noPrecision
	}
	return position
}

// readDateFormat reads a date format from its opening brace to its closing one, answering where it ends and
// whether it is closed; its first problem is kept for the directive's.
func readDateFormat(layoutRunes []rune, startPosition int, directive *formatDirective) (int, bool) {
	dateFormat := DateFormat{Parts: []DateFormatPart{}}
	var literalText strings.Builder
	position := startPosition
	keepProblem := func(problem error) {
		if directive.dateFormatProblem == nil {
			directive.dateFormatProblem = problem
		}
	}
	for position < len(layoutRunes) && layoutRunes[position] != '}' && layoutRunes[position] != '|' {
		if layoutRunes[position] != '%' {
			literalText.WriteRune(layoutRunes[position])
			position++
			continue
		}
		if position+1 < len(layoutRunes) && layoutRunes[position+1] == '%' {
			literalText.WriteRune('%')
			position += 2
			continue
		}
		dateFormatPart, endPosition, isComplete := readDateVerb(layoutRunes, position, keepProblem)
		if !isComplete {
			return endPosition, false
		}
		position = endPosition
		if literalText.Len() > 0 {
			dateFormat.Parts = append(dateFormat.Parts, DateFormatPart{Text: literalText.String()})
			literalText.Reset()
		}
		dateFormat.Parts = append(dateFormat.Parts, dateFormatPart)
	}
	if literalText.Len() > 0 {
		dateFormat.Parts = append(dateFormat.Parts, DateFormatPart{Text: literalText.String()})
	}
	if position < len(layoutRunes) && layoutRunes[position] == '|' {
		offsetStart := position + 1
		for position < len(layoutRunes) && layoutRunes[position] != '}' {
			position++
		}
		offsetText := string(layoutRunes[offsetStart:min(position, len(layoutRunes))])
		_, isOffset := offsetMinutesOf(offsetText)
		switch {
		case offsetText == "":
			keepProblem(errors.New("a date format's `|` takes an offset, such as `+08:00`"))
		case !isOffset:
			keepProblem(fmt.Errorf("%s is not an offset, such as `Z` or `+08:00`", CodeSpan(offsetText)))
		}
		dateFormat.Offset = &offsetText
	}
	if position >= len(layoutRunes) {
		return position, false
	}
	directive.DateFormat = &dateFormat
	return position + 1, true
}

// readDateVerb reads a date verb from its `%` through its letter, answering it, where it ends and whether the
// layout holds its letter; its first problem is kept.
func readDateVerb(
	layoutRunes []rune, startPosition int, keepProblem func(error),
) (DateFormatPart, int, bool) {
	directive := formatDirective{}
	position := readFlagsAndWidth(layoutRunes, startPosition+1, &directive)
	position = readPrecision(layoutRunes, position, &directive)
	if position >= len(layoutRunes) {
		return DateFormatPart{}, position, false
	}
	dateLetter := layoutRunes[position]
	fencedDateVerb := CodeSpan(string(layoutRunes[startPosition : position+1]))
	switch {
	case !strings.ContainsRune(dateVerbLetters, dateLetter):
		keepProblem(fmt.Errorf("%s is not a date verb `format` writes", fencedDateVerb))
	case directive.overPartLimit:
		keepProblem(fmt.Errorf("%s has a width or a precision over %d", fencedDateVerb, formatPartLimit))
	case directive.PlusSigned:
		keepProblem(fmt.Errorf("%s takes no `+` flag", fencedDateVerb))
	case directive.ZeroPadded && dateLetter == 'z':
		keepProblem(fmt.Errorf("%s takes no `0` flag", fencedDateVerb))
	case directive.Precision != nil:
		keepProblem(fmt.Errorf("%s takes no precision", fencedDateVerb))
	}
	return DateFormatPart{
		DateVerb:    dateLetter,
		LeftAligned: directive.LeftAligned,
		ZeroPadded:  directive.ZeroPadded,
		Width:       directive.Width,
	}, position + 1, true
}

// offsetMinutesOf reads a time offset: `Z`, or a sign, hours `00` to `23`, `:` and minutes `00` to `59`.
func offsetMinutesOf(offsetText string) (int, bool) {
	if offsetText == "Z" {
		return 0, true
	}
	offsetRunes := []rune(offsetText)
	isDigit := func(character rune) bool { return character >= '0' && character <= '9' }
	isWritten := len(offsetRunes) == 6 && (offsetRunes[0] == '+' || offsetRunes[0] == '-') && offsetRunes[3] == ':' &&
		isDigit(offsetRunes[1]) && isDigit(offsetRunes[2]) && isDigit(offsetRunes[4]) && isDigit(offsetRunes[5])
	if !isWritten {
		return 0, false
	}
	hours := int(offsetRunes[1]-'0')*10 + int(offsetRunes[2]-'0')
	minutes := int(offsetRunes[4]-'0')*10 + int(offsetRunes[5]-'0')
	offsetMinutes := hours*60 + minutes
	if offsetRunes[0] == '-' {
		offsetMinutes = -offsetMinutes
	}
	return offsetMinutes, hours <= 23 && minutes <= 59
}

func readFormatNumber(layoutRunes []rune, startPosition int, directive *formatDirective) (*int, int) {
	readNumber, position := 0, startPosition
	for position < len(layoutRunes) && layoutRunes[position] >= '0' && layoutRunes[position] <= '9' {
		if readNumber > formatPartLimit {
			directive.overPartLimit = true
		} else {
			readNumber = readNumber*10 + int(layoutRunes[position]-'0')
		}
		position++
	}
	if readNumber > formatPartLimit {
		directive.overPartLimit = true
	}
	if position == startPosition {
		return nil, position
	}
	return &readNumber, position
}

func directivePartProblem(directive formatDirective, verbRule formatVerbRule) error {
	fencedDirective := CodeSpan(directive.writtenText)
	switch {
	case directive.overPartLimit:
		return fmt.Errorf("%s has a width or a precision over %d", fencedDirective, formatPartLimit)
	case directive.LeftAligned && !verbRule.readsWidth:
		return fmt.Errorf("%s takes no `-` flag", fencedDirective)
	case directive.PlusSigned && !verbRule.readsPlusSign:
		return fmt.Errorf("%s takes no `+` flag", fencedDirective)
	case directive.ZeroPadded && !verbRule.readsZeroPadding:
		return fmt.Errorf("%s takes no `0` flag", fencedDirective)
	case directive.Width != nil && !verbRule.readsWidth:
		return fmt.Errorf("%s takes no width", fencedDirective)
	case directive.Precision != nil && !verbRule.readsPrecision:
		return fmt.Errorf("%s takes no precision", fencedDirective)
	case directive.DateFormat != nil && !verbRule.readsDateFormat:
		return fmt.Errorf("%s takes no date format", fencedDirective)
	case directive.Letter == 'B' && (directive.Precision == nil || *directive.Precision < 1):
		return fmt.Errorf("%s takes a precision of at least 1, such as `%%.64B`", fencedDirective)
	case directive.Letter == 'C' && directive.Precision != nil && *directive.Precision > maximumTimeDigits:
		return fmt.Errorf("%s takes a precision of at most %d, such as `.3` for milliseconds", fencedDirective, maximumTimeDigits)
	case verbRule.readsDateFormat && directive.DateFormat == nil:
		return fmt.Errorf("%s takes a date format, such as `%%{%%Y-%%m-%%d}C`", fencedDirective)
	}
	return nil
}

func formatOperandOf(operandValue any) formatOperand {
	switch typedValue := operandValue.(type) {
	case string:
		return formatOperand{kind: textOperand, kindName: "text", textValue: typedValue}
	case bool:
		return formatOperand{kind: booleanOperand, kindName: "a boolean", booleanValue: typedValue}
	case nil:
		return formatOperand{kind: otherOperand, kindName: "null"}
	}
	reflectedValue := reflect.ValueOf(operandValue)
	switch reflectedValue.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return formatOperand{kind: wholeOperand, kindName: wholeOperandKinds, wholeValue: reflectedValue.Int()}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		unsignedValue := reflectedValue.Uint()
		if unsignedValue <= math.MaxInt64 {
			return formatOperand{kind: wholeOperand, kindName: wholeOperandKinds, wholeValue: int64(unsignedValue)}
		}
		return formatOperand{kind: numberOperand, kindName: numberOperandKinds, numberValue: float64(unsignedValue)}
	case reflect.Float32, reflect.Float64:
		return numberFormatOperand(reflectedValue.Float())
	case reflect.Slice, reflect.Array:
		return formatOperand{kind: otherOperand, kindName: "a list"}
	case reflect.Map:
		return formatOperand{kind: otherOperand, kindName: "a map"}
	}
	return formatOperand{kind: otherOperand, kindName: fmt.Sprintf("a Go %T", operandValue)}
}

func numberFormatOperand(numberValue float64) formatOperand {
	if math.IsNaN(numberValue) || math.IsInf(numberValue, 0) {
		return formatOperand{kind: otherOperand, kindName: "a number that is not finite"}
	}
	if numberValue == math.Trunc(numberValue) && numberValue >= math.MinInt64 && numberValue < math.MaxInt64 {
		return formatOperand{kind: wholeOperand, kindName: wholeOperandKinds, wholeValue: int64(numberValue)}
	}
	return formatOperand{kind: numberOperand, kindName: numberOperandKinds, numberValue: numberValue}
}

func operandIsAccepted(verbRule formatVerbRule, operand formatOperand) bool {
	switch operand.kind {
	case textOperand:
		return verbRule.acceptsText
	case wholeOperand:
		return verbRule.acceptsWhole
	case numberOperand:
		return verbRule.acceptsNumber
	case booleanOperand:
		return verbRule.acceptsBoolean
	}
	return false
}

func formattedVerbText(
	layoutVerb LayoutVerb, verbRule formatVerbRule, operand formatOperand, operandNumber int,
) (string, error) {
	if !operandIsAccepted(verbRule, operand) {
		return "", fmt.Errorf(
			"%s writes %s, and operand %d is %s",
			CodeSpan("%"+string(layoutVerb.Letter)), verbRule.acceptedKinds, operandNumber, operand.kindName,
		)
	}
	readText := operand.textValue
	if layoutVerb.Precision != nil && layoutVerb.Letter != 'B' {
		readText = leadingCodePoints(readText, *layoutVerb.Precision)
	}
	switch layoutVerb.Letter {
	case 's':
		return readText, nil
	case 'q':
		return goQuotedText(readText), nil
	case 'u':
		return strings.Map(unicode.ToUpper, readText), nil
	case 'l':
		return strings.Map(unicode.ToLower, readText), nil
	case 'A':
		return strings.ReplaceAll(readText, "/", "_"), nil
	case 'B':
		return tailBoundedText(readText, *layoutVerb.Precision), nil
	case 'v':
		return valueVerbText(operand), nil
	case 'd':
		return signedWholeText(strconv.FormatInt(operand.wholeValue, 10), layoutVerb.PlusSigned), nil
	case 'x', 'X':
		return hexadecimalText(operand, layoutVerb.Letter == 'X'), nil
	case 'f':
		return signedWholeText(fixedPointText(operand, layoutVerb), layoutVerb.PlusSigned), nil
	}
	timeDigits := 0
	if layoutVerb.Precision != nil {
		timeDigits = *layoutVerb.Precision
	}
	return dateText(operand, timeDigits, *layoutVerb.DateFormat, operandNumber)
}

func leadingCodePoints(text string, count int) string {
	if utf8.RuneCountInString(text) <= count {
		return text
	}
	return string([]rune(text)[:count])
}

func tailBoundedText(text string, precision int) string {
	textRunes := []rune(text)
	if len(textRunes) <= precision {
		return text
	}
	return "-" + string(textRunes[len(textRunes)-(precision-1):])
}

func valueVerbText(operand formatOperand) string {
	switch operand.kind {
	case wholeOperand:
		return strconv.FormatInt(operand.wholeValue, 10)
	case booleanOperand:
		return strconv.FormatBool(operand.booleanValue)
	}
	return operand.textValue
}

func signedWholeText(numberText string, plusSigned bool) string {
	if plusSigned && !strings.HasPrefix(numberText, "-") {
		return "+" + numberText
	}
	return numberText
}

func hexadecimalText(operand formatOperand, upperCase bool) string {
	var hexadecimal string
	if operand.kind == textOperand {
		hexadecimal = fmt.Sprintf("%x", []byte(operand.textValue))
	} else {
		magnitude := uint64(operand.wholeValue)
		if operand.wholeValue < 0 {
			magnitude = uint64(-(operand.wholeValue + 1)) + 1
			hexadecimal = "-" + strconv.FormatUint(magnitude, 16)
		} else {
			hexadecimal = strconv.FormatUint(magnitude, 16)
		}
	}
	if upperCase {
		return strings.ToUpper(hexadecimal)
	}
	return hexadecimal
}

func fixedPointText(operand formatOperand, layoutVerb LayoutVerb) string {
	precision := 6
	if layoutVerb.Precision != nil {
		precision = *layoutVerb.Precision
	}
	if operand.kind == wholeOperand {
		wholeText := strconv.FormatInt(operand.wholeValue, 10)
		if precision == 0 {
			return wholeText
		}
		return wholeText + "." + strings.Repeat("0", precision)
	}
	return strconv.FormatFloat(operand.numberValue, 'f', precision, 64)
}

func dateText(operand formatOperand, timeDigits int, dateFormat DateFormat, operandNumber int) (string, error) {
	unitsPerSecond := int64(math.Pow10(timeDigits))
	var seconds, milliseconds int64
	if operand.kind == wholeOperand {
		seconds = operand.wholeValue / unitsPerSecond
		remainder := operand.wholeValue % unitsPerSecond
		if remainder < 0 {
			seconds, remainder = seconds-1, remainder+unitsPerSecond
		}
		milliseconds = remainder * 1000 / unitsPerSecond
	} else {
		timeInSeconds := operand.numberValue / float64(unitsPerSecond)
		flooredSeconds := math.Floor(timeInSeconds)
		if flooredSeconds < earliestFormatSeconds-secondsPerDay || flooredSeconds >= latestFormatSeconds+secondsPerDay {
			return "", dateRangeProblem(operandNumber)
		}
		seconds = int64(flooredSeconds)
		milliseconds = int64(math.Floor((timeInSeconds - flooredSeconds) * 1000))
	}
	if seconds < earliestFormatSeconds-secondsPerDay || seconds >= latestFormatSeconds+secondsPerDay {
		return "", dateRangeProblem(operandNumber)
	}
	writtenOffset := "Z"
	if dateFormat.Offset != nil && *dateFormat.Offset != "+00:00" {
		writtenOffset = *dateFormat.Offset
	}
	offsetMinutes, _ := offsetMinutesOf(writtenOffset)
	seconds += int64(offsetMinutes) * 60
	if seconds < earliestFormatSeconds || seconds >= latestFormatSeconds {
		return "", dateRangeProblem(operandNumber)
	}
	days, secondOfDay := seconds/secondsPerDay, seconds%secondsPerDay
	if secondOfDay < 0 {
		days, secondOfDay = days-1, secondOfDay+secondsPerDay
	}
	year, month, day := civilDateOfDays(days)
	dateVerbValues := map[rune]int64{
		'Y': year,
		'y': year % 100,
		'm': month,
		'd': day,
		'H': secondOfDay / 3600,
		'M': secondOfDay % 3600 / 60,
		'S': secondOfDay % 60,
		'L': milliseconds,
	}
	var writtenDate strings.Builder
	for _, dateFormatPart := range dateFormat.Parts {
		switch dateFormatPart.DateVerb {
		case 0:
			writtenDate.WriteString(dateFormatPart.Text)
		case 'z':
			writtenDate.WriteString(paddedText(writtenOffset, dateFormatPart.Width, dateFormatPart.LeftAligned, false))
		default:
			dateVerbText := strconv.FormatInt(dateVerbValues[dateFormatPart.DateVerb], 10)
			writtenDate.WriteString(paddedText(
				dateVerbText, dateFormatPart.Width, dateFormatPart.LeftAligned, dateFormatPart.ZeroPadded,
			))
		}
	}
	return writtenDate.String(), nil
}

func dateRangeProblem(operandNumber int) error {
	return fmt.Errorf("`%%C` writes a time in the years 0000 to 9999, and operand %d is outside them", operandNumber)
}

func civilDateOfDays(days int64) (int64, int64, int64) {
	shiftedDays := days + 719468
	era := shiftedDays / 146097
	if shiftedDays%146097 < 0 {
		era--
	}
	dayOfEra := shiftedDays - era*146097
	yearOfEra := (dayOfEra - dayOfEra/1460 + dayOfEra/36524 - dayOfEra/146096) / 365
	dayOfYear := dayOfEra - (365*yearOfEra + yearOfEra/4 - yearOfEra/100)
	shiftedMonth := (5*dayOfYear + 2) / 153
	day := dayOfYear - (153*shiftedMonth+2)/5 + 1
	month := shiftedMonth + 3
	if shiftedMonth >= 10 {
		month = shiftedMonth - 9
	}
	year := yearOfEra + era*400
	if month <= 2 {
		year++
	}
	return year, month, day
}

func goQuotedText(text string) string {
	var quotedText strings.Builder
	quotedText.WriteByte('"')
	for _, textRune := range text {
		switch {
		case textRune == '"':
			quotedText.WriteString(`\"`)
		case textRune == '\\':
			quotedText.WriteString(`\\`)
		case textRune == '\a':
			quotedText.WriteString(`\a`)
		case textRune == '\b':
			quotedText.WriteString(`\b`)
		case textRune == '\f':
			quotedText.WriteString(`\f`)
		case textRune == '\n':
			quotedText.WriteString(`\n`)
		case textRune == '\r':
			quotedText.WriteString(`\r`)
		case textRune == '\t':
			quotedText.WriteString(`\t`)
		case textRune == '\v':
			quotedText.WriteString(`\v`)
		case textRune < 0x20 || textRune == 0x7f:
			fmt.Fprintf(&quotedText, `\x%02x`, textRune)
		case textRune >= 0x80 && textRune <= 0x9f:
			fmt.Fprintf(&quotedText, `\u%04x`, textRune)
		default:
			quotedText.WriteRune(textRune)
		}
	}
	quotedText.WriteByte('"')
	return quotedText.String()
}

func paddedVerbText(layoutVerb LayoutVerb, verbRule formatVerbRule, verbText string) string {
	return paddedText(
		verbText, layoutVerb.Width, layoutVerb.LeftAligned, layoutVerb.ZeroPadded && verbRule.readsZeroPadding,
	)
}

// paddedText is text padded to a width: on the right where left-aligned, else with leading zeros after its sign
// where zero-padded, else with spaces on the left.
func paddedText(text string, width *int, leftAligned, zeroPadded bool) string {
	if width == nil {
		return text
	}
	padding := *width - utf8.RuneCountInString(text)
	if padding <= 0 {
		return text
	}
	switch {
	case leftAligned:
		return text + strings.Repeat(" ", padding)
	case zeroPadded:
		signText := ""
		if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") {
			signText, text = text[:1], text[1:]
		}
		return signText + strings.Repeat("0", padding) + text
	}
	return strings.Repeat(" ", padding) + text
}
