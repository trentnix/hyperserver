package blog

import "bytes"

// validMP3 checks two complete Layer III frames after an optional ID3v2 tag.
// It accepts standard CBR and VBR headers, not free-format bitrates. This checks
// the file format without decoding audio or trusting a filename or MIME header.
func validMP3(data []byte) bool {
	if bytes.HasPrefix(data, []byte("ID3")) {
		if len(data) < 10 || data[3] < 2 || data[3] > 4 || data[4] == 255 {
			return false
		}
		size := 0
		for _, b := range data[6:10] {
			if b >= 128 {
				return false
			}
			size = size<<7 | int(b)
		}
		offset := 10 + size
		if data[3] == 4 && data[5]&0x10 != 0 {
			offset += 10 // An ID3v2.4 footer is outside the tag size.
		}
		if offset > len(data) {
			return false
		}
		data = data[offset:]
	}
	first := mp3FrameSize(data)
	if first == 0 || first > len(data)-4 {
		return false
	}
	second := mp3FrameSize(data[first:])
	return second > 0 && first+second <= len(data) && data[1]&0x18 == data[first+1]&0x18 && data[2]&0x0c == data[first+2]&0x0c
}

func mp3FrameSize(data []byte) int {
	if len(data) < 4 || data[0] != 255 || data[1]&0xe0 != 0xe0 || data[1]&6 != 2 {
		return 0
	}
	version, rate, sample := (data[1]>>3)&3, data[2]>>4, (data[2]>>2)&3
	if version == 1 || rate == 0 || rate == 15 || sample == 3 || data[3]&3 == 2 {
		return 0
	}
	bitrate := [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}[rate]
	sampleRate := [...]int{44100, 48000, 32000}[sample]
	coefficient := 144000
	if version != 3 {
		bitrate = [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}[rate]
		sampleRate /= 2
		coefficient = 72000
		if version == 0 {
			sampleRate /= 2
		}
	}
	return coefficient*bitrate/sampleRate + int((data[2]>>1)&1)
}
