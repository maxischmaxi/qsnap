package diff

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"

	"github.com/corona10/goimagehash"
	"github.com/maxischmaxi/qsnap/internal/logging"
	"github.com/nfnt/resize"
	"go.uber.org/zap"
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
		logging.L.Error("failed to open PNG", zap.String("path", path), zap.Error(err))
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		logging.L.Error("failed to decode PNG", zap.String("path", path), zap.Error(err))
		return nil, err
	}
	return img, nil
}

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		logging.L.Error("failed to create PNG", zap.String("path", path), zap.Error(err))
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
		logging.L.Error("size mismatch after resize", zap.Int("aWidth", ab.Dx()), zap.Int("aHeight", ab.Dy()), zap.Int("bWidth", bb.Dx()), zap.Int("bHeight", bb.Dy()))
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

	logging.L.Debug("pixel diff computed", zap.Int("width", w), zap.Int("height", h), zap.Int("diffCount", diffCount), zap.Float64("ratio", ratio), zap.Float64("threshold", threshold))

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
		logging.L.Error("failed to compute pHash for image A", zap.Error(err))
		return PHashResult{}, err
	}
	hb, err := goimagehash.PerceptionHash(bSmall)
	if err != nil {
		logging.L.Error("failed to compute pHash for image B", zap.Error(err))
		return PHashResult{}, err
	}

	hd, err := ha.Distance(hb)
	if err != nil {
		logging.L.Error("failed to compute pHash distance", zap.Error(err))
		return PHashResult{}, err
	}

	logging.L.Debug("pHash distance computed", zap.Int("hammingDistance", hd), zap.Int("allowed", allowed))
	return PHashResult{
		Pass:            hd <= allowed,
		HammingDistance: hd,
	}, nil
}

func CompareFiles(baselinePath, outPath string, pxThreshold float64, phThreshold int) (PixelResult, PHashResult, error) {
	logging.L.Info("comparing images", zap.String("baseline", baselinePath), zap.String("output", outPath), zap.Float64("pixelThreshold", pxThreshold), zap.Int("pHashThreshold", phThreshold))

	baseImg, err := openPNG(baselinePath)
	if err != nil {
		logging.L.Error("failed to open baseline image", zap.String("path", baselinePath), zap.Error(err))
		return PixelResult{}, PHashResult{}, err
	}
	outImg, err := openPNG(outPath)
	if err != nil {
		logging.L.Error("failed to open output image", zap.String("path", outPath), zap.Error(err))
		return PixelResult{}, PHashResult{}, err
	}

	px, diffImg, err := pixelDiff(baseImg, outImg, math.Max(0, pxThreshold))
	if err != nil {
		logging.L.Error("failed to compute pixel diff", zap.String("baseline", baselinePath), zap.String("output", outPath), zap.Error(err))
		return PixelResult{}, PHashResult{}, err
	}

	var ph PHashResult
	ph, err = pHashDistance(baseImg, outImg, phThreshold)
	if err != nil {
		logging.L.Error("failed to compute pHash diff", zap.String("baseline", baselinePath), zap.String("output", outPath), zap.Error(err))
		return PixelResult{}, PHashResult{}, err
	}

	if !px.Pass {
		_ = savePNG(outPath+".diff.png", diffImg)
		px.DiffImagePath = outPath + ".diff.png"
	}

	logging.L.Info("diff result",
		zap.String("baseline", baselinePath),
		zap.String("output", outPath),
		zap.Float64("pixelDiffRatio", px.RatioDiff),
		zap.Bool("pixelDiffPass", px.Pass),
		zap.String("pixelDiffImage", px.DiffImagePath),
		zap.Int("pHashHammingDistance", ph.HammingDistance),
		zap.Bool("pHashPass", ph.Pass),
	)
	return px, ph, nil
}
