package main

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/emicklei/go-restful"
	"github.com/gographics/imagick/imagick"
)

// https://github.com/gographics/imagick
// https://github.com/nfnt/resize
// sudo port install pkgconfig
// sudo port install imagemagick
// alt: https://github.com/maddox/magick-installer/blob/master/magick-installer.sh
// http://www.graphicsmagick.org/benchmarks.html

// convert --version

type ImageResizer struct {
	home string
}

func (i ImageResizer) AddWebService() {
	ws := new(restful.WebService)
	ws.Consumes("*/*")
	ws.Produces("image/jpeg;image/png;image/webp")
	ws.Route(ws.GET("/images/{id}").To(i.resizeImagick))
	restful.Add(ws)
}

func (i ImageResizer) resizeImagick(req *restful.Request, resp *restful.Response) {
	now := time.Now()
	var err error
	source := req.PathParameter("id")
	query_width := req.QueryParameter("w")
	if len(query_width) == 0 {
		query_width = "1000"
	}
	result_width, err := strconv.Atoi(query_width)
	if err != nil {
		resp.WriteErrorString(500, err.Error())
		return
	}

	mw := imagick.NewMagickWand()
	// Schedule cleanup
	defer mw.Destroy()

	err = mw.ReadImage("http://node5.kluster.local.nl.bol.com:8765/3,4e22fbf1e638")
	if err != nil {
		resp.WriteErrorString(500, err.Error())
		return
	}
	// Get original logo size
	width := mw.GetImageWidth()
	height := mw.GetImageHeight()
	ratio := float64(height) / float64(width)

	// Calculate half the size
	hWidth := uint(result_width)
	hHeight := uint(float64(result_width) * ratio)

	// Resize the image using the Lanczos filter
	// The blur factor is a float, where > 1 is blurry, < 1 is sharp
	err = mw.ResizeImage(hWidth, hHeight, imagick.FILTER_LANCZOS, 0.8)
	if err != nil {
		resp.WriteErrorString(500, err.Error())
		return
	}

	// Set the compression quality to 95 (high quality = low compression)
	err = mw.SetImageCompressionQuality(95)
	if err != nil {
		resp.WriteErrorString(500, err.Error())
		return
	}

	resp.Header().Set("Content-Type", "image/webp")
	err = mw.SetImageFormat("webp")
	if err != nil {
		resp.WriteErrorString(500, err.Error())
		return
	}

	data := mw.GetImagesBlob()
	_, err = resp.ResponseWriter.Write(data)
	if err != nil {
		resp.WriteErrorString(500, err.Error())
		return
	}

	log.Printf("resized %s to width=%s in %v", source, query_width, (time.Now().Sub(now)))
}

func main() {
	imagick.Initialize()
	// Schedule cleanup
	defer imagick.Terminate()

	//resizer := ImageResizer{home: "/Volumes/GDE/PM-Image/DVD/Stills/"} // "/Users/emicklei/Downloads/"}
	resizer := ImageResizer{home: "/Users/emicklei/Downloads/"}
	resizer.AddWebService()

	log.Printf("start listening on localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
