# Embedded Fonts

These font files are embedded into the Go binary at compile time via
`go:embed`, together with their license, `OFL.txt` in this directory.

## Ponomar Project Fonts (SIL OFL 1.1)

The following fonts are from the [Ponomar Project](https://sci.ponomar.net/),
each in its own repository under https://github.com/slavonic/. All are
licensed under the [SIL Open Font License 1.1](https://openfontlicense.org/);
each font's copyright notice, as the font itself has it, is in `OFL.txt`.

| Font | Description | Repository |
|------|-------------|------------|
| Ponomar | Main text font, modern Synodal style | [slavonic/ponomar](https://github.com/slavonic/ponomar) |
| Indiction | Decorative drop caps, Synodal style | [slavonic/Indiction](https://github.com/slavonic/Indiction) |
| Vertograd | Drop caps/titling, late 19th century | [slavonic/Vertograd](https://github.com/slavonic/Vertograd) |
| Triodion | Text font, Synodal style variant | [slavonic/Triodion](https://github.com/slavonic/Triodion) |
| Cathisma | Titling font, Holy Synod style | [slavonic/Cathisma](https://github.com/slavonic/Cathisma) |
| Oglavie | Titling font, 18th–20th century | [slavonic/Oglavie](https://github.com/slavonic/Oglavie) |
| Pomorsky | Old Ritualist calligraphic style | [slavonic/Pomorsky](https://github.com/slavonic/Pomorsky) |
| Fedorovsk | 16th century Ivan Fedorov style | [slavonic/Fedorovsk](https://github.com/slavonic/Fedorovsk) |
| Menaion | Ustav-era manuscript style | [slavonic/Menaion](https://github.com/slavonic/Menaion) |
| Monomakh | Academic mixed style | [slavonic/Monomakh](https://github.com/slavonic/Monomakh) |

Two of the fonts' own copyright strings name another project: Fedorovsk's
the Pochaevsk Project, Menaion's the Monomakh repository. They are quoted
in `OFL.txt` as the fonts have them.

## Carlito (SIL OFL 1.1)

The Carlito family (Regular, Italic, Bold, Bold Italic) is a Calibri-compatible
sans-serif used for plain-language (Russian, English, etc.) instructions and rubrics.

Source: https://github.com/googlefonts/carlito
License: [SIL Open Font License 1.1](https://openfontlicense.org/), with
Reserved Font Name "Carlito"

## Bukvica (not embedded)

Bukvica is a decorative initials font from [irmologion.ru](https://irmologion.ru/).
Its own notice says "free for non-commercial use", with no license to
redistribute it, so it is **not** included in this directory. If needed,
download it and place `Bukvica.ttf` here, in `fonts/data/`, before
building: the binary then embeds it (`fonts.go`). The repository ignores
the file.
