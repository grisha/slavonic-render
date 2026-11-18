# Ornament sources

The SVG each ornament PDF in `../data/` is made from: `ornament-NAME.svg`
here is the source of `../data/ornament-NAME-red.pdf`, and `NAME` is what
a text writes (`![](NAME)`).

The PDF renderer uses the PDFs. The HTML renderer uses these files: they
are embedded in the binary (`assets.SVG`), and `htmlrender` writes one
into a page as an `<svg>` element. They are also what an ornament is made
again from, or made from in another colour or another format.

Every file is black (one `fill="#000000"`) on the canvas of its PDF: the
PDF's page, in points, is the SVG's `viewBox`.

The HTML renderer relies on that form: it writes a file from its `<svg`
tag on, and turns `fill="#000000"` into `fill="currentColor"` so that the
page's style sheet gives the colour. A new ornament must have the same
form (the tests of `htmlrender` check every file here), and a PDF in
`../data/` must have its SVG here (the test of `assets`).

## Making the PDF

boxesandglue embeds PDF, not SVG, and its PDF reader (gofpdi) needs PDF
1.4, which is why Ghostscript follows `rsvg-convert`. The three `-dOmit…`
switches keep the date of the run, a document ID and the XMP metadata out
of the file. On Fedora/RHEL the tools are in `librsvg2-tools` and
`ghostscript`.

```sh
n=leafs
sed 's/fill="#000000"/fill="#CC0000"/g' ornament-$n.svg > /tmp/$n-red.svg
rsvg-convert -f pdf /tmp/$n-red.svg -o /tmp/$n-tmp.pdf
gs -dSAFER -dBATCH -dNOPAUSE -sDEVICE=pdfwrite -dCompatibilityLevel=1.4 \
   -dOmitInfoDate=true -dOmitID=true -dOmitXMP=true \
   -sOutputFile=../data/ornament-$n-red.pdf /tmp/$n-tmp.pdf
```

For each ornament, the PDF this makes has the page size of the one in
`../data/` and the same pixels at 200 dpi.

The red in the command is the red of the ornament PDFs. It differs
slightly from the red of the text (`cu.Kinovar`).

## Where each came from

The ornaments are from two collections of public-domain vector graphics,
[FreeSVG.org](https://freesvg.org) and
[PublicDomainVectors.org](https://publicdomainvectors.org). Each was
chosen because its page there marked it as public domain. The page that
each one came from was not recorded.

All but `leafs` are bitmap tracings and carry potrace 1.15's mark; `leafs`
has the same form, but its metadata says only "This image is Public
Domain". Six stand on a 1280 × 640 canvas (`leafs`, `leafs3`, `line1`,
`line2`, `line3`, `panel`); four of those were then cropped by narrowing
the `viewBox`, the paths untouched.

| File | Canvas | Note |
|---|---|---|
| `ornament-leaf2.svg` | 1088 × 280 | |
| `ornament-leafs.svg` | 1280 × 210 | cropped from 1280 × 640 (`viewBox="0 215 1280 210"`); marked "This image is Public Domain" |
| `ornament-leafs3.svg` | 1280 × 640 | not cropped: the page has white space above and below |
| `ornament-leafs4.svg` | 1280 × 416 | |
| `ornament-line1.svg` | 1280 × 114 | cropped from 1280 × 640 (`viewBox="0 263 1280 114"`) |
| `ornament-line2.svg` | 1280 × 65 | cropped from 1280 × 640 (`viewBox="0 285 1280 65"`) |
| `ornament-line3.svg` | 1280 × 149 | cropped from 1280 × 640 (`viewBox="0 245 1280 149"`) |
| `ornament-panel.svg` | 1280 × 640 | not cropped |
| `ornament-upleft.svg` | 780 × 1280 | |
