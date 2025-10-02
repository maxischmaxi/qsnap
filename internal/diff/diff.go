package diff

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"

	"github.com/corona10/goimagehash"
	"github.com/nfnt/resize"
)

type PixelResult struct {
	Pass          bool    `json:"pass"`
	RatioDiff     float64 `json:"ratioDiff"`     // fraction of differing pixels
	DiffImagePath string  `json:"diffImagePath"` // "" if not generated
}

type PHashResult struct {
	Pass            bool `json:"pass"`
	HammingDistance int  `json:"hammingDistance"`
}

func openPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	return img, nil
}

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func pixelDiff(a, b image.Image, threshold float64) (PixelResult, image.Image, error) {
	ab := a.Bounds()
	bb := b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		// scale B to A
		b = resize.Resize(uint(ab.Dx()), uint(ab.Dy()), b, resize.NearestNeighbor)
		bb = b.Bounds()
	}
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		return PixelResult{}, nil, errors.New("size mismatch after resize")
	}

	w, h := ab.Dx(), ab.Dy()
	diffImg := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(diffImg, diffImg.Bounds(), a, ab.Min, draw.Src)

	var diffCount int
	for y := range h {
		for x := range w {
			ar, ag, ab2, aa := a.At(x, y).RGBA()
			br, bg, bb2, ba := b.At(x, y).RGBA()

			if ar != br || ag != bg || ab2 != bb2 || aa != ba {
				diffCount++
				// mark in magenta
				diffImg.Set(x, y, color.RGBA{255, 0, 255, 255})
			}
		}
	}
	total := w * h
	ratio := float64(diffCount) / float64(total)

	return PixelResult{
		Pass:      ratio <= threshold,
		RatioDiff: ratio,
	}, diffImg, nil
}

func pHashDistance(a, b image.Image, allowed int) (PHashResult, error) {
	// resize to a stable small size
	aSmall := resize.Resize(256, 0, a, resize.Lanczos3)
	bSmall := resize.Resize(256, 0, b, resize.Lanczos3)

	ha, err := goimagehash.PerceptionHash(aSmall)
	if err != nil {
		return PHashResult{}, err
	}
	hb, err := goimagehash.PerceptionHash(bSmall)
	if err != nil {
		return PHashResult{}, err
	}

	hd, err := ha.Distance(hb)
	if err != nil {
		return PHashResult{}, err
	}

	return PHashResult{
		Pass:            hd <= allowed,
		HammingDistance: hd,
	}, nil
}

func CompareFiles(baselinePath string, buf []byte, diffPath string, pxThreshold float64, phThreshold int) (PixelResult, PHashResult, error) {
	baseImg, err := openPNG(baselinePath)
	if err != nil {
		return PixelResult{}, PHashResult{}, err
	}

	reader := bytes.NewReader(buf)
	img, err := png.Decode(reader)
	if err != nil {
		return PixelResult{}, PHashResult{}, err
	}

	px, diffImg, err := pixelDiff(baseImg, img, math.Max(0, pxThreshold))
	if err != nil {
		return PixelResult{}, PHashResult{}, err
	}

	var ph PHashResult
	ph, err = pHashDistance(baseImg, img, phThreshold)
	if err != nil {
		return PixelResult{}, PHashResult{}, err
	}

	if !px.Pass {
		_ = savePNG(diffPath, diffImg)
		px.DiffImagePath = diffPath
	}

	return px, ph, nil
}
