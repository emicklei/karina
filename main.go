package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/emicklei/go-restful"
	"github.com/garyburd/redigo/redis"
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
	weedMasterHostPort string
	redisHostPort      string
	redisConn          redis.Conn
	hosts              map[string]string
}

// {"locations":[{"publicUrl":"node5.kluster.local.nl.bol.com:8765","url":"localhost:8765"}]}

type WeedLookUp struct {
	Locations []struct {
		PublicUrl string `json:"publicUrl"`
		Url       string `json:"url"`
	} `json:"locations"`
}

func (i ImageResizer) AddWebService() {
	ws := new(restful.WebService)
	ws.Consumes("*/*")
	ws.Produces("image/jpeg;image/png;image/webp")
	ws.Route(ws.GET("/images/{id}").To(i.resizeImagick))
	restful.Add(ws)
}

func (i *ImageResizer) reconnectRedis() {
	i.disconnectRedis()
	log.Printf("Connecting to Redis on %s\n", i.redisHostPort)
	c, err := redis.Dial("tcp", i.redisHostPort)
	if err != nil {
		log.Fatal(err)
	}
	i.redisConn = c
}

func (i *ImageResizer) disconnectRedis() {
	if i.redisConn != nil {
		i.redisConn.Close()
		i.redisConn = nil
	}
}

func (i ImageResizer) lookupUrl(id string) string {
	// 4,4ebc1e7836ea
	parts := strings.Split(id, ",")
	volume := parts[0]
	host, ok := i.hosts[volume]
	if !ok {
		resp, err := http.Get(fmt.Sprintf("http://%s/dir/lookup?volumeId=%s", i.weedMasterHostPort, volume))
		if err != nil {
			return ""
		}
		defer resp.Body.Close()
		lookup := WeedLookUp{}
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return ""
		}
		json.Unmarshal(data, &lookup)
		host = lookup.Locations[0].PublicUrl
		i.hosts[volume] = host
	}
	return fmt.Sprintf("http://%s/%s", host, id)
}

func (i ImageResizer) resizeImagick(req *restful.Request, resp *restful.Response) {
	now := time.Now()
	var err error
	id := req.PathParameter("id")
	query_width := req.QueryParameter("w")
	if len(query_width) == 0 {
		query_width = "1000"
	}
	result_width, err := strconv.Atoi(query_width)
	if err != nil {
		resp.WriteErrorString(500, "Cannot convert w:"+err.Error())
		return
	}

	mw := imagick.NewMagickWand()
	// Schedule cleanup
	defer mw.Destroy()

	reply, err := i.redisConn.Do("GET", id)
	if err != nil {
		resp.WriteErrorString(500, "Redis GET failed:"+err.Error())
		return
	}
	if reply == nil {
		resp.WriteErrorString(404, "Redis says: no such id:"+id)
		return
	}
	fid := string(reply.([]byte))
	url := i.lookupUrl(fid)

	if len(url) == 0 {
		resp.WriteErrorString(500, "Volume lookup failed")
		return
	}
	log.Printf("id:%s -> url:%s", id, url)

	err = mw.ReadImage(url)
	if err != nil {
		resp.WriteErrorString(500, "Read image failed:"+err.Error())
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
		resp.WriteErrorString(500, "Resize image failed:"+err.Error())
		return
	}

	// Set the compression quality to 95 (high quality = low compression)
	err = mw.SetImageCompressionQuality(95)
	if err != nil {
		resp.WriteErrorString(500, "Set Compression failed:"+err.Error())
		return
	}

	resp.Header().Set("Content-Type", "image/webp")
	err = mw.SetImageFormat("webp")
	if err != nil {
		resp.WriteErrorString(500, "Set image format failed:"+err.Error())
		return
	}

	data := mw.GetImagesBlob()
	_, err = resp.ResponseWriter.Write(data)
	if err != nil {
		resp.WriteErrorString(500, err.Error())
		return
	}

	log.Printf("resized %s (%s) to width=%s in %v", id, fid, query_width, (time.Now().Sub(now)))
}

func main() {
	imagick.Initialize()
	// Schedule cleanup
	defer imagick.Terminate()

	resizer := ImageResizer{
		redisHostPort:      "10.10.135.91:6379",
		weedMasterHostPort: "node5.kluster.local.nl.bol.com:9333",
		hosts:              map[string]string{}}
	resizer.reconnectRedis()
	resizer.AddWebService()
	defer resizer.disconnectRedis()

	log.Printf("start listening on localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
