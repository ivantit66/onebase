package sheet

import _ "embed"

// Встроенные шрифты для PDF-рендера (план 64, этап 2). PT Serif и PT Sans —
// SIL Open Font License 1.1 (см. fonts/OFL.txt), родная кириллица, визуально
// совместимы с Times New Roman / Arial. Регистрируются в fpdf через
// AddUTF8FontFromBytes; экспортированы, чтобы legacy printform/pdf.go (этап 4
// его удалит) мог использовать те же байты без дублирования.
//
// PT Sans поставляется только в начертаниях Regular/Bold: для курсивного
// «sans» PDF-рендер падает на PT Serif Italic (см. pdf.go, resolveFont).

//go:embed fonts/PTSerif-Regular.ttf
var PTSerifRegular []byte

//go:embed fonts/PTSerif-Bold.ttf
var PTSerifBold []byte

//go:embed fonts/PTSerif-Italic.ttf
var PTSerifItalic []byte

//go:embed fonts/PTSerif-BoldItalic.ttf
var PTSerifBoldItalic []byte

//go:embed fonts/PTSans-Regular.ttf
var PTSansRegular []byte

//go:embed fonts/PTSans-Bold.ttf
var PTSansBold []byte
