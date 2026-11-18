// glyphextents.go - Extract actual ink bounding boxes from font glyphs
//
// This file provides functions to get the actual ink boundaries of glyphs,
// needed for precise drop cap positioning. boxesandglue provides metrics based
// on font-wide values (ascender/descender), not actual glyph bounds.
//
// OpenType fonts store outlines in two formats:
// 1. TrueType (glyf table) - has xMin/yMin/xMax/yMax in glyph headers
// 2. CFF (CFF table) - requires interpreting charstring bytecode
//
// Most Church Slavonic fonts (Ponomar, Indiction, Vertograd) use CFF outlines.

package pdfrender

import (
	"encoding/binary"

	pdf "github.com/boxesandglue/baseline-pdf"
	"github.com/boxesandglue/textshape/ot"
)

// glyphExtents contains the ink bounding box for a glyph in font units.
type glyphExtents struct {
	xMin int16 // Left edge
	yMin int16 // Bottom edge
	xMax int16 // Right edge
	yMax int16 // Top edge
}

// getGlyphExtents returns the ink bounding box for a glyph.
// Works for both TrueType (glyf table) and CFF fonts.
func getGlyphExtents(face *pdf.Face, glyphID int) (glyphExtents, bool) {
	otFace := face.OTFace()
	if otFace == nil || otFace.Font == nil {
		return glyphExtents{}, false
	}

	if otFace.IsCFF() {
		return getCFFGlyphExtents(otFace.Font, ot.GlyphID(glyphID))
	}
	return getTrueTypeGlyphExtents(otFace.Font, ot.GlyphID(glyphID))
}

// getGlyphIDForRune returns the glyph ID for a Unicode codepoint.
func getGlyphIDForRune(face *pdf.Face, r rune) (int, bool) {
	otFace := face.OTFace()
	if otFace == nil {
		return 0, false
	}
	cmap := otFace.Cmap()
	if cmap == nil {
		return 0, false
	}
	gid, ok := cmap.Lookup(ot.Codepoint(r))
	return int(gid), ok
}

// getTrueTypeGlyphExtents reads glyph extents from the glyf table.
func getTrueTypeGlyphExtents(font *ot.Font, gid ot.GlyphID) (glyphExtents, bool) {
	locaData, err := font.TableData(ot.TagLoca)
	if err != nil {
		return glyphExtents{}, false
	}

	headData, err := font.TableData(ot.TagHead)
	if err != nil || len(headData) < 54 {
		return glyphExtents{}, false
	}
	indexToLocFormat := int16(binary.BigEndian.Uint16(headData[50:]))

	maxpData, err := font.TableData(ot.TagMaxp)
	if err != nil || len(maxpData) < 6 {
		return glyphExtents{}, false
	}
	numGlyphs := int(binary.BigEndian.Uint16(maxpData[4:]))

	if int(gid) >= numGlyphs {
		return glyphExtents{}, false
	}

	var offset, length uint32
	if indexToLocFormat == 0 {
		idx := int(gid) * 2
		if idx+4 > len(locaData) {
			return glyphExtents{}, false
		}
		offset = uint32(binary.BigEndian.Uint16(locaData[idx:])) * 2
		nextOffset := uint32(binary.BigEndian.Uint16(locaData[idx+2:])) * 2
		length = nextOffset - offset
	} else {
		idx := int(gid) * 4
		if idx+8 > len(locaData) {
			return glyphExtents{}, false
		}
		offset = binary.BigEndian.Uint32(locaData[idx:])
		nextOffset := binary.BigEndian.Uint32(locaData[idx+4:])
		length = nextOffset - offset
	}

	if length == 0 {
		return glyphExtents{}, false
	}

	glyfData, err := font.TableData(ot.TagGlyf)
	if err != nil {
		return glyphExtents{}, false
	}

	if int(offset)+10 > len(glyfData) {
		return glyphExtents{}, false
	}

	data := glyfData[offset:]
	xMin := int16(binary.BigEndian.Uint16(data[2:]))
	yMin := int16(binary.BigEndian.Uint16(data[4:]))
	xMax := int16(binary.BigEndian.Uint16(data[6:]))
	yMax := int16(binary.BigEndian.Uint16(data[8:]))

	return glyphExtents{xMin: xMin, yMin: yMin, xMax: xMax, yMax: yMax}, true
}

// getCFFGlyphExtents calculates glyph extents by interpreting the CFF charstring.
func getCFFGlyphExtents(font *ot.Font, gid ot.GlyphID) (glyphExtents, bool) {
	cffData, err := font.TableData(ot.TagCFF)
	if err != nil {
		return glyphExtents{}, false
	}

	cff, err := ot.ParseCFF(cffData)
	if err != nil {
		return glyphExtents{}, false
	}

	if int(gid) >= cff.NumGlyphs() {
		return glyphExtents{}, false
	}

	charstring := cff.CharStrings[gid]
	if len(charstring) == 0 {
		return glyphExtents{}, false
	}

	interp := newBoundsInterpreter(cff.GlobalSubrs, cff.LocalSubrs)
	if err := interp.execute(charstring); err != nil {
		return glyphExtents{}, false
	}

	if !interp.hasPoints {
		return glyphExtents{}, false
	}

	return glyphExtents{
		xMin: int16(interp.xMin),
		yMin: int16(interp.yMin),
		xMax: int16(interp.xMax),
		yMax: int16(interp.yMax),
	}, true
}

// boundsInterpreter interprets CFF Type 2 charstrings to calculate bounding box.
type boundsInterpreter struct {
	globalSubrs [][]byte
	localSubrs  [][]byte
	globalBias  int
	localBias   int

	x, y                   float64
	xMin, yMin, xMax, yMax float64
	hasPoints              bool

	stack     []float64
	hintCount int
}

func newBoundsInterpreter(globalSubrs, localSubrs [][]byte) *boundsInterpreter {
	return &boundsInterpreter{
		globalSubrs: globalSubrs,
		localSubrs:  localSubrs,
		globalBias:  calcSubrBias(len(globalSubrs)),
		localBias:   calcSubrBias(len(localSubrs)),
		stack:       make([]float64, 0, 48),
	}
}

func calcSubrBias(count int) int {
	if count < 1240 {
		return 107
	}
	if count < 33900 {
		return 1131
	}
	return 32768
}

func (b *boundsInterpreter) updateBounds(x, y float64) {
	if !b.hasPoints {
		b.xMin, b.xMax = x, x
		b.yMin, b.yMax = y, y
		b.hasPoints = true
		return
	}
	if x < b.xMin {
		b.xMin = x
	}
	if x > b.xMax {
		b.xMax = x
	}
	if y < b.yMin {
		b.yMin = y
	}
	if y > b.yMax {
		b.yMax = y
	}
}

func (b *boundsInterpreter) moveTo(dx, dy float64) {
	b.x += dx
	b.y += dy
	b.updateBounds(b.x, b.y)
}

func (b *boundsInterpreter) lineTo(dx, dy float64) {
	b.x += dx
	b.y += dy
	b.updateBounds(b.x, b.y)
}

func (b *boundsInterpreter) curveTo(dx1, dy1, dx2, dy2, dx3, dy3 float64) {
	x1 := b.x + dx1
	y1 := b.y + dy1
	x2 := x1 + dx2
	y2 := y1 + dy2
	x3 := x2 + dx3
	y3 := y2 + dy3

	p0x, p0y := b.x, b.y
	p1x, p1y := x1, y1
	p2x, p2y := x2, y2
	p3x, p3y := x3, y3

	for _, t := range []float64{0, 0.25, 0.5, 0.75, 1.0} {
		t2 := t * t
		t3 := t2 * t
		mt := 1 - t
		mt2 := mt * mt
		mt3 := mt2 * mt

		x := mt3*p0x + 3*mt2*t*p1x + 3*mt*t2*p2x + t3*p3x
		y := mt3*p0y + 3*mt2*t*p1y + 3*mt*t2*p2y + t3*p3y
		b.updateBounds(x, y)
	}

	b.x = x3
	b.y = y3
}

func (b *boundsInterpreter) execute(data []byte) error {
	pos := 0

	for pos < len(data) {
		byte0 := data[pos]

		if byte0 >= 32 || byte0 == 28 || byte0 == 255 {
			val, consumed := b.decodeOperand(data[pos:])
			b.stack = append(b.stack, val)
			pos += consumed
			continue
		}

		op := int(byte0)
		pos++

		if byte0 == 12 && pos < len(data) {
			op = 12<<8 | int(data[pos])
			pos++
		}

		switch op {
		case 21: // rmoveto
			if len(b.stack) >= 2 {
				dy := b.stack[len(b.stack)-1]
				dx := b.stack[len(b.stack)-2]
				b.moveTo(dx, dy)
			}
			b.stack = b.stack[:0]

		case 22: // hmoveto
			if len(b.stack) >= 1 {
				dx := b.stack[len(b.stack)-1]
				b.moveTo(dx, 0)
			}
			b.stack = b.stack[:0]

		case 4: // vmoveto
			if len(b.stack) >= 1 {
				dy := b.stack[len(b.stack)-1]
				b.moveTo(0, dy)
			}
			b.stack = b.stack[:0]

		case 5: // rlineto
			for i := 0; i+1 < len(b.stack); i += 2 {
				b.lineTo(b.stack[i], b.stack[i+1])
			}
			b.stack = b.stack[:0]

		case 6: // hlineto
			for i := 0; i < len(b.stack); i++ {
				if i%2 == 0 {
					b.lineTo(b.stack[i], 0)
				} else {
					b.lineTo(0, b.stack[i])
				}
			}
			b.stack = b.stack[:0]

		case 7: // vlineto
			for i := 0; i < len(b.stack); i++ {
				if i%2 == 0 {
					b.lineTo(0, b.stack[i])
				} else {
					b.lineTo(b.stack[i], 0)
				}
			}
			b.stack = b.stack[:0]

		case 8: // rrcurveto
			for i := 0; i+5 < len(b.stack); i += 6 {
				b.curveTo(b.stack[i], b.stack[i+1], b.stack[i+2], b.stack[i+3], b.stack[i+4], b.stack[i+5])
			}
			b.stack = b.stack[:0]

		case 24: // rcurveline
			n := len(b.stack)
			if n >= 8 {
				for i := 0; i+5 < n-2; i += 6 {
					b.curveTo(b.stack[i], b.stack[i+1], b.stack[i+2], b.stack[i+3], b.stack[i+4], b.stack[i+5])
				}
				b.lineTo(b.stack[n-2], b.stack[n-1])
			}
			b.stack = b.stack[:0]

		case 25: // rlinecurve
			n := len(b.stack)
			if n >= 8 {
				for i := 0; i+1 < n-6; i += 2 {
					b.lineTo(b.stack[i], b.stack[i+1])
				}
				b.curveTo(b.stack[n-6], b.stack[n-5], b.stack[n-4], b.stack[n-3], b.stack[n-2], b.stack[n-1])
			}
			b.stack = b.stack[:0]

		case 26: // vvcurveto
			i := 0
			if len(b.stack)%4 == 1 {
				b.curveTo(b.stack[0], b.stack[1], b.stack[2], b.stack[3], 0, b.stack[4])
				i = 5
			}
			for ; i+3 < len(b.stack); i += 4 {
				b.curveTo(0, b.stack[i], b.stack[i+1], b.stack[i+2], 0, b.stack[i+3])
			}
			b.stack = b.stack[:0]

		case 27: // hhcurveto
			i := 0
			if len(b.stack)%4 == 1 {
				b.curveTo(b.stack[1], b.stack[0], b.stack[2], b.stack[3], b.stack[4], 0)
				i = 5
			}
			for ; i+3 < len(b.stack); i += 4 {
				b.curveTo(b.stack[i], 0, b.stack[i+1], b.stack[i+2], b.stack[i+3], 0)
			}
			b.stack = b.stack[:0]

		case 30: // vhcurveto
			b.alternatingCurves(true)
			b.stack = b.stack[:0]

		case 31: // hvcurveto
			b.alternatingCurves(false)
			b.stack = b.stack[:0]

		case 10: // callsubr (local)
			if len(b.stack) > 0 {
				subrNum := int(b.stack[len(b.stack)-1]) + b.localBias
				b.stack = b.stack[:len(b.stack)-1]
				if subrNum >= 0 && subrNum < len(b.localSubrs) {
					if err := b.execute(b.localSubrs[subrNum]); err != nil {
						return err
					}
				}
			}

		case 29: // callgsubr (global)
			if len(b.stack) > 0 {
				subrNum := int(b.stack[len(b.stack)-1]) + b.globalBias
				b.stack = b.stack[:len(b.stack)-1]
				if subrNum >= 0 && subrNum < len(b.globalSubrs) {
					if err := b.execute(b.globalSubrs[subrNum]); err != nil {
						return err
					}
				}
			}

		case 11: // return
			return nil

		case 14: // endchar
			return nil

		case 1, 3, 18, 23: // hstem, vstem, hstemhm, vstemhm
			b.hintCount += len(b.stack) / 2
			b.stack = b.stack[:0]

		case 19, 20: // hintmask, cntrmask
			if len(b.stack) > 0 {
				b.hintCount += len(b.stack) / 2
				b.stack = b.stack[:0]
			}
			maskBytes := (b.hintCount + 7) / 8
			pos += maskBytes

		case 12<<8 | 34: // hflex
			if len(b.stack) >= 7 {
				b.curveTo(b.stack[0], 0, b.stack[1], b.stack[2], b.stack[3], 0)
				b.curveTo(b.stack[4], 0, b.stack[5], 0, b.stack[6], 0)
			}
			b.stack = b.stack[:0]

		case 12<<8 | 35: // flex
			if len(b.stack) >= 12 {
				b.curveTo(b.stack[0], b.stack[1], b.stack[2], b.stack[3], b.stack[4], b.stack[5])
				b.curveTo(b.stack[6], b.stack[7], b.stack[8], b.stack[9], b.stack[10], b.stack[11])
			}
			b.stack = b.stack[:0]

		case 12<<8 | 36: // hflex1
			if len(b.stack) >= 9 {
				b.curveTo(b.stack[0], b.stack[1], b.stack[2], b.stack[3], b.stack[4], 0)
				b.curveTo(b.stack[5], 0, b.stack[6], b.stack[7], b.stack[8], 0)
			}
			b.stack = b.stack[:0]

		case 12<<8 | 37: // flex1
			if len(b.stack) >= 11 {
				dx := b.stack[0] + b.stack[2] + b.stack[4] + b.stack[6] + b.stack[8]
				dy := b.stack[1] + b.stack[3] + b.stack[5] + b.stack[7] + b.stack[9]
				if absFloat(dx) > absFloat(dy) {
					b.curveTo(b.stack[0], b.stack[1], b.stack[2], b.stack[3], b.stack[4], b.stack[5])
					b.curveTo(b.stack[6], b.stack[7], b.stack[8], b.stack[9], b.stack[10], -dy)
				} else {
					b.curveTo(b.stack[0], b.stack[1], b.stack[2], b.stack[3], b.stack[4], b.stack[5])
					b.curveTo(b.stack[6], b.stack[7], b.stack[8], b.stack[9], -dx, b.stack[10])
				}
			}
			b.stack = b.stack[:0]

		default:
			b.stack = b.stack[:0]
		}
	}

	return nil
}

func (b *boundsInterpreter) alternatingCurves(startVertical bool) {
	n := len(b.stack)
	i := 0
	vertical := startVertical

	for i+3 < n {
		if i+4 == n-1 {
			if vertical {
				b.curveTo(0, b.stack[i], b.stack[i+1], b.stack[i+2], b.stack[i+3], b.stack[i+4])
			} else {
				b.curveTo(b.stack[i], 0, b.stack[i+1], b.stack[i+2], b.stack[i+4], b.stack[i+3])
			}
			break
		}

		if vertical {
			b.curveTo(0, b.stack[i], b.stack[i+1], b.stack[i+2], b.stack[i+3], 0)
		} else {
			b.curveTo(b.stack[i], 0, b.stack[i+1], b.stack[i+2], 0, b.stack[i+3])
		}
		i += 4
		vertical = !vertical
	}
}

func (b *boundsInterpreter) decodeOperand(data []byte) (float64, int) {
	if len(data) == 0 {
		return 0, 0
	}

	b0 := data[0]

	if b0 >= 32 && b0 <= 246 {
		return float64(int(b0) - 139), 1
	}

	if b0 >= 247 && b0 <= 250 {
		if len(data) < 2 {
			return 0, 1
		}
		return float64((int(b0)-247)*256 + int(data[1]) + 108), 2
	}

	if b0 >= 251 && b0 <= 254 {
		if len(data) < 2 {
			return 0, 1
		}
		return float64(-(int(b0)-251)*256 - int(data[1]) - 108), 2
	}

	if b0 == 28 {
		if len(data) < 3 {
			return 0, 1
		}
		v := int16(binary.BigEndian.Uint16(data[1:3]))
		return float64(v), 3
	}

	if b0 == 255 {
		if len(data) < 5 {
			return 0, 1
		}
		v := int32(binary.BigEndian.Uint32(data[1:5]))
		return float64(v) / 65536.0, 5
	}

	return 0, 1
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
