package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/image/draw"
)

const (
	photoLimit        = 25 << 20
	storedPhotoLimit  = 5 << 20
	videoLimit        = 20 << 20
	mediaRequestLimit = 81 << 20
	maxPhotos         = 6
)

var mediaSlots = make(chan struct{}, 2)

type repairMedia struct {
	kind, contentType string
	data              []byte
	digest            [sha256.Size]byte
}

type mediaEntry struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

func readMediaPart(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, &Problem{413, "media_size", "上传总大小超出限制"}
		}
		return nil, bad("上传中断，请重新提交")
	}
	if int64(len(data)) > limit {
		return nil, &Problem{413, "media_size", "单个文件超出大小限制（原图 25 MB，视频 20 MB）"}
	}
	return data, nil
}

func readOrderMedia(w http.ResponseWriter, r *http.Request, ref *draftConfirmation) (Input, []repairMedia, error) {
	var input Input
	r.Body = http.MaxBytesReader(w, r.Body, mediaRequestLimit)
	reader, err := r.MultipartReader()
	if err != nil {
		return input, nil, bad("附件上传格式无效")
	}
	fields := map[string]*string{"category": &input.Category, "location": &input.Location, "description": &input.Description}
	refs := map[string]*int64{"source_order_id": &input.SourceOrderID, "source_version": &input.SourceVersion, "draft_message_id": &ref.MessageID, "draft_version": &ref.Version}
	media := make([]repairMedia, 0, maxPhotos+1)
	var photos, videos int
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return input, nil, &Problem{413, "media_size", "上传总大小超出限制"}
			}
			return input, nil, bad("上传中断，请重新提交")
		}
		name := part.FormName()
		if target := refs[name]; target != nil && part.FileName() == "" {
			raw, err := readMediaPart(part, 20)
			if err != nil {
				return input, nil, err
			}
			n, err := strconv.ParseInt(string(raw), 10, 64)
			if err != nil || n < 1 {
				return input, nil, bad("工单关联、草稿标识或版本无效")
			}
			*target = n
			delete(refs, name)
			continue
		}
		if field := fields[name]; field != nil && part.FileName() == "" {
			data, err := readMediaPart(part, 8200)
			if err != nil {
				return input, nil, err
			}
			*field = string(data)
			delete(fields, name)
			continue
		}
		limit := photoLimit
		switch {
		case name == "photo" && part.FileName() != "" && photos < maxPhotos:
			photos++
		case name == "video" && part.FileName() != "" && videos == 0:
			videos++
			limit = videoLimit
		default:
			return input, nil, bad("每单最多上传 6 张照片和 1 段视频，请检查所选文件")
		}
		raw, err := readMediaPart(part, int64(limit))
		if err != nil {
			return input, nil, err
		}
		item := repairMedia{kind: name, digest: sha256.Sum256(raw)}
		if name == "photo" {
			item.data, item.contentType, err = normalizePhoto(raw)
		} else {
			item.data, err = normalizeVideo(r.Context(), raw)
			item.contentType = "video/mp4"
		}
		if err != nil {
			return input, nil, err
		}
		media = append(media, item)
	}
	if (ref.MessageID == 0) != (ref.Version == 0) {
		return input, nil, bad("缺少草稿标识或版本")
	}
	if (input.SourceOrderID == 0) != (input.SourceVersion == 0) {
		return input, nil, bad("缺少原工单标识或版本")
	}
	return input, media, nil
}

func normalizePhoto(data []byte) ([]byte, string, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, "", bad("照片无法读取，请使用 JPEG 或 PNG")
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 64_000_000 {
		return nil, "", bad("照片分辨率过高，请选用 6400 万像素以内的照片")
	}
	if format == "png" && int64(config.Width)*int64(config.Height) > 24_000_000 {
		return nil, "", bad("PNG 分辨率过高，请使用 JPEG 或较小的 PNG")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", bad("照片已损坏，请重新选择")
	}
	if format == "jpeg" {
		decoded = orientJPEG(decoded, jpegOrientation(data))
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	const edge = 2560
	if max(width, height) > edge {
		longest := max(width, height)
		width, height = max(1, width*edge/longest), max(1, height*edge/longest)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(pixels, pixels.Bounds(), image.White, image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(pixels, pixels.Bounds(), decoded, bounds, draw.Over, nil)
	var out bytes.Buffer
	for _, quality := range []int{86, 72, 58} {
		out.Reset()
		if err := jpeg.Encode(&out, pixels, &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", err
		}
		if out.Len() <= storedPhotoLimit {
			return out.Bytes(), "image/jpeg", nil
		}
	}
	return nil, "", bad("照片压缩后仍超出处理限制，请使用较小的图片")
}

// Read only the orientation tag in the first EXIF directory. The JPEG encoder
// writes new pixels without copying any source metadata.
func jpegOrientation(data []byte) uint16 {
	for offset := 2; offset+4 <= len(data) && data[offset] == 0xff; {
		marker := data[offset+1]
		if marker == 0xda || marker == 0xd9 {
			break
		}
		size := int(binary.BigEndian.Uint16(data[offset+2:]))
		if size < 2 || offset+2+size > len(data) {
			break
		}
		segment := data[offset+4 : offset+2+size]
		if marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			tiff := segment[6:]
			if len(tiff) < 8 {
				break
			}
			var order binary.ByteOrder
			switch string(tiff[:2]) {
			case "II":
				order = binary.LittleEndian
			case "MM":
				order = binary.BigEndian
			default:
				return 1
			}
			if order.Uint16(tiff[2:]) != 42 {
				break
			}
			start := int(order.Uint32(tiff[4:]))
			if start < 8 || start > len(tiff)-2 {
				break
			}
			count := int(order.Uint16(tiff[start:]))
			for pos := start + 2; count > 0 && pos <= len(tiff)-12; pos, count = pos+12, count-1 {
				entry := tiff[pos : pos+12]
				if order.Uint16(entry) == 0x112 && order.Uint16(entry[2:]) == 3 && order.Uint32(entry[4:]) == 1 {
					return order.Uint16(entry[8:])
				}
			}
		}
		offset += 2 + size
	}
	return 1
}

type orientedPhoto struct {
	image.Image
	orientation uint16
}

func orientJPEG(img image.Image, orientation uint16) image.Image {
	if orientation < 2 || orientation > 8 {
		return img
	}
	return orientedPhoto{img, orientation}
}

func (p orientedPhoto) Bounds() image.Rectangle {
	size := p.Image.Bounds().Size()
	if p.orientation >= 5 {
		size.X, size.Y = size.Y, size.X
	}
	return image.Rectangle{Max: size}
}

func (p orientedPhoto) At(x, y int) color.Color {
	bounds := p.Image.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	switch p.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	return p.Image.At(x+bounds.Min.X, y+bounds.Min.Y)
}

type videoInfo struct {
	Format  struct{ Duration string } `json:"format"`
	Streams []struct {
		Type     string `json:"codec_type"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Duration string `json:"duration"`
	} `json:"streams"`
}

func probeVideo(ctx context.Context, path string) (videoInfo, error) {
	var info videoInfo
	command := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-max_alloc", "134217728", "-max_streams", "8", "-protocol_whitelist", "file",
		"-f", "mov", "-enable_drefs", "0", "-use_absolute_path", "0",
		"-show_entries", "format=duration:stream=codec_type,width,height,duration", "-of", "json", "-i", path)
	command.Env = []string{"LANG=C"}
	data, err := command.Output()
	if err != nil {
		return info, err
	}
	return info, json.Unmarshal(data, &info)
}

func checkVideo(info videoInfo) error {
	duration, err := strconv.ParseFloat(info.Format.Duration, 64)
	if err != nil || !(duration > 0) {
		return bad("视频时长无法读取，请重新导出视频")
	}
	if duration > 15 {
		return bad("视频超过 15 秒，请剪短后重新上传")
	}
	videos := 0
	for _, stream := range info.Streams {
		if stream.Type != "video" && stream.Type != "audio" {
			continue
		}
		if d, err := strconv.ParseFloat(stream.Duration, 64); err == nil && d > 15 {
			return bad("视频超过 15 秒，请剪短后重新上传")
		}
		if stream.Type == "video" {
			videos++
			if stream.Width < 1 || stream.Height < 1 || stream.Width > 4096 || stream.Height > 4096 || int64(stream.Width)*int64(stream.Height) > 9_000_000 {
				return bad("视频分辨率过高，请使用 4K 以内的视频")
			}
		}
	}
	if videos != 1 {
		return bad("请选择仅含一条画面的 MP4 或 MOV 视频")
	}
	return nil
}

func normalizeVideo(ctx context.Context, data []byte) ([]byte, error) {
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		return nil, bad("视频无法读取，请使用 MP4 或 MOV")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, err
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "repair-video-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	source, target := filepath.Join(dir, "source"), filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(source, data, 0600); err != nil {
		return nil, err
	}
	info, err := probeVideo(ctx, source)
	if err != nil {
		return nil, bad("视频已损坏或无法读取，请重新导出 MP4 或 MOV")
	}
	if err := checkVideo(info); err != nil {
		return nil, err
	}

	command := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-xerror", "-max_alloc", "134217728",
		"-protocol_whitelist", "file", "-max_streams", "8", "-threads", "1", "-max_pixels", "9000000",
		"-f", "mov", "-enable_drefs", "0", "-use_absolute_path", "0", "-i", source,
		"-map", "0:v:0", "-map", "0:a:0?", "-map_metadata", "-1", "-map_chapters", "-1",
		"-filter_threads", "1", "-vf", `scale=min(1280\,iw):min(1280\,ih):force_original_aspect_ratio=decrease:force_divisible_by=2,fps=30,setsar=1`,
		"-t", "15.1", "-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p", "-crf", "25",
		"-maxrate", "4M", "-bufsize", "8M", "-threads", "1", "-c:a", "aac", "-ac", "2", "-b:a", "96k",
		"-movflags", "+faststart", "-fs", strconv.Itoa(videoLimit+1), target)
	command.Env = []string{"LANG=C"}
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, bad("视频处理超时，请降低分辨率后重试")
		}
		return nil, bad("视频无法转换，请重新导出后上传")
	}
	info, err = probeVideo(ctx, target)
	if err != nil {
		return nil, bad("视频处理后无法播放，请重新导出")
	}
	if err := checkVideo(info); err != nil {
		return nil, err
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readMediaPart(file, videoLimit)
}

func (s *Server) listOrderMedia(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	filter, args := mediaOrderFilter(current(r))
	rows, err := s.DB.QueryContext(r.Context(), "SELECT m.id,m.kind,m.content_type,OCTET_LENGTH(m.content) FROM work_orders LEFT JOIN work_order_media m ON m.work_order_id=work_orders.id WHERE "+filter+" AND work_orders.id=? ORDER BY m.id", append(args, id)...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []mediaEntry{}
	authorized := false
	for rows.Next() {
		authorized = true
		var id, size sql.NullInt64
		var kind, contentType sql.NullString
		if err := rows.Scan(&id, &kind, &contentType, &size); err != nil {
			s.fail(w, r, err)
			return
		}
		if id.Valid {
			items = append(items, mediaEntry{ID: id.Int64, Kind: kind.String, ContentType: contentType.String, Size: size.Int64})
		}
	}
	if err := rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	if !authorized {
		s.fail(w, r, invisible())
		return
	}
	send(w, 200, map[string]any{"items": items})
}

func mediaOrderFilter(user User) (string, []any) {
	switch user.Role {
	case "admin", "requester", "technician":
		return orderFilter(user)
	default:
		return "1=0", nil
	}
}

func (s *Server) orderMediaFile(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	mediaID, err := strconv.ParseInt(r.PathValue("media"), 10, 64)
	if err != nil || mediaID < 1 {
		s.fail(w, r, invisible())
		return
	}
	filter, args := mediaOrderFilter(current(r))
	var contentType string
	var data []byte
	err = s.DB.QueryRowContext(r.Context(), "SELECT m.content_type,m.content FROM work_orders JOIN work_order_media m ON m.work_order_id=work_orders.id WHERE "+filter+" AND work_orders.id=? AND m.id=?", append(args, id, mediaID)...).Scan(&contentType, &data)
	if err != nil {
		s.fail(w, r, dbError(err))
		return
	}
	ext := map[string]string{"image/png": "png", "image/jpeg": "jpg", "video/mp4": "mp4"}[contentType]
	if ext == "" {
		s.fail(w, r, errors.New("unknown media type"))
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"repair-%d.%s\"", mediaID, ext))
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}
