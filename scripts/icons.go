//go:build ignore

// Run from the repository root to rebuild the geometric app mark.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	if err := os.MkdirAll("web/public/icons", 0755); err != nil {
		panic(err)
	}
	for _, size := range []int{192, 512} {
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		for y := range size {
			for x := range size {
				px, py := float64(x)/float64(size), float64(y)/float64(size)
				mark := px >= .28 && px <= .38 && py >= .24 && py <= .70
				dx, dy := px-.43, py-.55
				outer := dx*dx+dy*dy < .155*.155
				inner := dx*dx+dy*dy < .064*.064
				mark = mark || outer && !inner
				dx, dy = px-.66, py-.655
				mark = mark || dx*dx+dy*dy < .045*.045
				c := color.RGBA{25, 28, 34, 255}
				if mark {
					c = color.RGBA{179, 232, 135, 255}
				}
				img.SetRGBA(x, y, c)
			}
		}
		path := fmt.Sprintf("web/public/icons/icon-%d.png", size)
		f, err := os.Create(path)
		if err != nil {
			panic(err)
		}
		if err = png.Encode(f, img); err != nil {
			panic(err)
		}
		if err = f.Close(); err != nil {
			panic(err)
		}
	}
}
