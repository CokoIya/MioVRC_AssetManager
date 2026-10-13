package archive

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"vrclib/internal/core"
)

// The volumes of an archive (1.7.13). BOOTH's downloads come split two ways: numbered pieces in the way of 7-Zip
// ("x.zip.001", "x.7z.002", "x.unitypackage.001", "x.001"), unpacked from the first, and a zip spanned the way
// ZIP itself does it ("x.z01", "x.z02" … with "x.zip", which is the last of them and the one that opens them).
// RAR has "x.part1.rar" … and the old "x.rar" with "x.r00" …. A volume left out of a download is the most
// common reason such an archive does not open — and the programs that unpack it then call it damaged. So the
// volumes are counted before anything is unpacked, and what is missing is named: by the gaps in the numbers,
// and where the archive says how many it has (a 7z's start header, a spanned zip's end record) or whether it
// goes on (the end of a split zip, a RAR's end block), also at the end. Pieces in the way of 7-Zip of a zip or
// a unitypackage are joined here, so that they unpack without an archive program too.

// volSet: the volumes of one set, by their numbers (1 is the one that comes first).
type volSet struct {
	kind  string         // as volKey: "num.zip", "num.", "part", "zip", "rar"
	stem  string         // the set's name, as its files write it
	inner string         // num.*: what the pieces are of, as written (".zip", ".unitypackage", "")
	width int            // the digits of a number as written ("001": 3)
	ext   string         // part: ".rar" as written; zip: ".z" as written; rar: ".r" as written
	byNum map[int]string // number → file
	end   string         // a spanned zip: its .zip, which comes after the numbered pieces
}

func readVolSet(a archiveSet) volSet {
	v := volSet{byNum: map[int]string{}}
	for _, p := range a.Parts {
		base := filepath.Base(p)
		stem, kind, _ := volKey(base)
		if v.kind == "" {
			v.kind, v.stem = kind, stem
		}
		if kind != v.kind {
			continue
		}
		if m := reVolNum.FindStringSubmatch(base); m != nil && strings.HasPrefix(kind, "num.") {
			n, _ := strconv.Atoi(m[3])
			v.byNum[n], v.inner, v.width = p, m[2], len(m[3])
			continue
		}
		if m := reVolPart.FindStringSubmatch(base); m != nil && kind == "part" {
			n, _ := strconv.Atoi(m[2])
			i := strings.LastIndex(strings.ToLower(base), ".part")
			v.byNum[n], v.width, v.ext = p, len(base)-i-len(".part")-4, base[len(base)-4:]
			continue
		}
		if m := reVolZnn.FindStringSubmatch(base); m != nil && kind == "zip" {
			n, _ := strconv.Atoi(m[2])
			v.byNum[n], v.ext = p, base[len(m[1]):len(m[1])+2]
			continue
		}
		if m := reVolRnn.FindStringSubmatch(base); m != nil && kind == "rar" {
			n, _ := strconv.Atoi(m[2])
			v.byNum[n+2], v.ext = p, base[len(m[1]):len(m[1])+2] // x.rar is the first, x.r00 the second
			continue
		}
		switch kind {
		case "zip":
			v.end = p
		case "rar":
			v.byNum[1] = p
		}
	}
	if v.ext == "" {
		v.ext = map[string]string{"part": ".rar", "zip": ".z", "rar": ".r"}[v.kind]
	}
	return v
}

// name: the file name of volume n.
func (v volSet) name(n int) string {
	switch v.kind {
	case "part":
		return v.stem + ".part" + fmt.Sprintf("%0*d", max(v.width, 1), n) + v.ext
	case "zip":
		return v.stem + v.ext + fmt.Sprintf("%02d", n)
	case "rar":
		if n == 1 {
			return v.stem + ".rar"
		}
		return v.stem + v.ext + fmt.Sprintf("%02d", n-2)
	}
	return v.stem + v.inner + "." + fmt.Sprintf("%0*d", max(v.width, 3), n)
}

func (v volSet) numbers() []int {
	var nums []int
	for n := range v.byNum {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums
}

// pieces: what the pieces are of — "zip", "7z", "rar", "unitypackage", or "" when the name and the first
// piece do not say.
func (v volSet) pieces() string {
	switch strings.ToLower(strings.TrimPrefix(v.inner, ".")) {
	case "zip":
		return "zip"
	case "7z":
		return "7z"
	case "rar":
		return "rar"
	case "unitypackage":
		return "unitypackage"
	}
	switch magicOf(v.byNum[1]) {
	case "zip":
		return "zip"
	case "7z":
		return "7z"
	case "rar":
		return "rar"
	case "gzip":
		return "unitypackage"
	}
	return ""
}

// missingVolumes: what a set is known to be missing, as the player is told it; "" when nothing is.
func missingVolumes(a archiveSet) string {
	if len(a.Parts) == 0 {
		return ""
	}
	v := readVolSet(a)
	split := strings.HasPrefix(v.kind, "num.") || v.kind == "part"
	if v.kind == "zip" && v.end == "" {
		return fmt.Sprintf("缺少 %s：.z01、.z02 这样的分卷要和同名的 .zip 放在同一个文件夹，解压从 .zip 开始（.zip 是最后一卷）", v.stem+".zip")
	}
	if !split && len(v.byNum) <= 1 && (v.kind != "zip" || len(v.byNum) == 0) {
		return "" // an archive in one file
	}
	nums := v.numbers()
	last, total := 0, 0
	if len(nums) > 0 {
		last = nums[len(nums)-1]
	}
	found := len(nums)
	switch {
	case v.kind == "zip":
		if n := zipDisks(v.end); n > 0 {
			total, last = n+1, max(last, n) // the pieces, then the .zip
		}
		found++
	case split && v.pieces() == "7z":
		if n := sevenZipVolumes(v.byNum[1]); n > 0 {
			total, last = n, max(last, n)
		}
	}
	var miss []string
	for n := 1; n <= last; n++ {
		if v.byNum[n] == "" {
			miss = append(miss, v.name(n))
		}
	}
	if len(miss) > 0 {
		if total > 0 {
			return fmt.Sprintf("缺少分卷：%s（共 %d 卷，找到 %d 卷），请把全部分卷下载到同一个文件夹后再解压", strings.Join(miss, "、"), total, found)
		}
		return fmt.Sprintf("缺少分卷：%s（找到 %d 卷），请把全部分卷下载到同一个文件夹后再解压", strings.Join(miss, "、"), found)
	}
	// the end: a split zip ends with its end record, a RAR volume says whether it is the last
	more := false
	switch {
	case total > 0:
	case split && v.pieces() == "zip":
		more = !hasZipEnd(v.byNum[last]) && sameSize(v.byNum[last], v.byNum[1])
	case v.kind == "part" || v.kind == "rar" || (split && v.pieces() == "rar"):
		more = rarContinues(v.byNum[last])
	}
	if more {
		return cutShort(v, last)
	}
	return ""
}

// cutShort: the volumes after the last one there are missing.
func cutShort(v volSet, last int) string {
	return fmt.Sprintf("缺少后面的分卷：从 %s 起（找到 %d 卷），请把全部分卷下载到同一个文件夹后再解压", v.name(last+1), len(v.byNum))
}

func sameSize(a, b string) bool {
	sa, ea := os.Stat(a)
	sb, eb := os.Stat(b)
	return ea == nil && eb == nil && sa.Size() == sb.Size()
}

// magicOf: what a file is by its first bytes ("zip", "7z", "rar", "gzip"; "" when unknown).
func magicOf(p string) string {
	if p == "" {
		return ""
	}
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, 8)
	n, _ := io.ReadFull(f, b)
	b = b[:n]
	switch {
	case bytes.HasPrefix(b, []byte("PK\x03\x04")), bytes.HasPrefix(b, []byte("PK\x07\x08")):
		return "zip"
	case bytes.HasPrefix(b, []byte("7z\xbc\xaf\x27\x1c")):
		return "7z"
	case bytes.HasPrefix(b, []byte("Rar!\x1a\x07")):
		return "rar"
	case bytes.HasPrefix(b, []byte{0x1f, 0x8b}):
		return "gzip"
	}
	return ""
}

// sevenZipVolumes: into how many pieces of the size of the first a 7z was split (its start header says how
// long the whole archive is); 0 when it cannot be told.
func sevenZipVolumes(first string) int {
	f, err := os.Open(first)
	if err != nil {
		return 0
	}
	defer f.Close()
	h := make([]byte, 32)
	if _, err := io.ReadFull(f, h); err != nil || !bytes.HasPrefix(h, []byte("7z\xbc\xaf\x27\x1c")) {
		return 0
	}
	next := binary.LittleEndian.Uint64(h[12:20])
	size := binary.LittleEndian.Uint64(h[20:28])
	fi, err := f.Stat()
	if err != nil || fi.Size() <= 32 || next > 1<<50 || size > 1<<40 {
		return 0
	}
	whole, vol := 32+next+size, uint64(fi.Size())
	return int((whole + vol - 1) / vol)
}

// tailOf: the last n bytes of a file.
func tailOf(p string, n int64) []byte {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	at := max(fi.Size()-n, 0)
	b := make([]byte, fi.Size()-at)
	if _, err := f.ReadAt(b, at); err != nil && err != io.EOF {
		return nil
	}
	return b
}

// hasZipEnd: the file ends a zip (the end of central directory record is in its last bytes).
func hasZipEnd(p string) bool {
	return bytes.LastIndex(tailOf(p, 66000), []byte("PK\x05\x06")) >= 0
}

// zipDisks: the number of numbered pieces of a zip spanned over several (x.z01 …): the disk its end record is
// on; 0 when it cannot be told.
func zipDisks(zipPath string) int {
	b := tailOf(zipPath, 66000)
	i := bytes.LastIndex(b, []byte("PK\x05\x06"))
	if i < 0 || i+8 > len(b) {
		return 0
	}
	disk := int(binary.LittleEndian.Uint16(b[i+4 : i+6]))
	if disk == 0xffff { // ZIP64: the locator before the record says how many disks there are in all
		if j := bytes.LastIndex(b[:i], []byte("PK\x06\x07")); j >= 0 && j+20 <= len(b) {
			return int(binary.LittleEndian.Uint32(b[j+16:j+20])) - 1
		}
		return 0
	}
	return disk
}

// rarContinues: a RAR volume that says the archive goes on in the next one (RAR 5: the end block's flags;
// RAR 4: the end block's flag). false when it cannot be told.
func rarContinues(p string) bool {
	b := tailOf(p, 32)
	if n := len(b); n >= 4 && b[n-4] == 0x03 && b[n-3] == 0x05 && b[n-2]&^0x04 == 0 { // RAR 5: size 3, type 5, header flags
		return b[n-1]&0x01 != 0
	}
	if n := len(b); n >= 7 && b[n-5] == 0x7b && binary.LittleEndian.Uint16(b[n-2:]) == 7 { // RAR 4: CRC, 0x7b, flags, size 7
		return binary.LittleEndian.Uint16(b[n-4:n-2])&0x0001 != 0
	}
	return false
}

var (
	// how archive programs name a volume they did not find
	reMissingVol = regexp.MustCompile(`(?im)(?:missing volume|cannot find volume|can't find volume|找不到卷|找不到分卷)\s*[:：]?\s*(.+?)\s*$`)
	reCutShort   = regexp.MustCompile(`(?i)unexpected end of (?:archive|data)|unexpected end|is not the last volume|need(?:s)? (?:next|the next) volume`)
	reDamaged    = regexp.MustCompile(`(?i)crc failed|data error|headers error|checksum error|is corrupt|damaged`)
)

// toolSays: what an archive program's output says about a set that did not unpack, as the player is told it;
// "" when it says nothing of volumes or damage.
func toolSays(out string, a archiveSet) string {
	var names []string
	for _, m := range reMissingVol.FindAllStringSubmatch(out, -1) {
		n := filepath.Base(strings.Trim(strings.ReplaceAll(m[1], `\`, "/"), `"' `))
		if n != "" && n != "." && !core.ContainsStr(names, n) {
			names = append(names, n)
		}
	}
	v := readVolSet(a)
	switch {
	case len(names) > 0:
		return fmt.Sprintf("缺少分卷：%s（找到 %d 卷），请把全部分卷下载到同一个文件夹后再解压", strings.Join(names, "、"), len(v.byNum)+map[bool]int{true: 1, false: 0}[v.end != ""])
	case len(a.Parts) > 1 && reCutShort.MatchString(out):
		nums := v.numbers()
		if len(nums) > 0 && v.kind != "zip" {
			return cutShort(v, nums[len(nums)-1])
		}
		return "分卷不完整，可能缺少分卷，请把全部分卷下载到同一个文件夹后再解压"
	case reDamaged.MatchString(out):
		if len(a.Parts) > 1 {
			return "压缩包数据校验失败：可能有分卷没下载完整或缺少分卷，请重新下载有问题的分卷"
		}
		return "压缩包数据校验失败：文件可能没下载完整，请重新下载"
	}
	return ""
}

// ---------- pieces joined here ----------

// joinable: what the pieces of a set in the way of 7-Zip are of, when they are joined here (a zip or a
// unitypackage, every piece there); "" otherwise.
func joinable(a archiveSet) string {
	v := readVolSet(a)
	if !strings.HasPrefix(v.kind, "num.") || v.byNum[1] == "" {
		return ""
	}
	if p := v.pieces(); p == "zip" || p == "unitypackage" {
		return p
	}
	return ""
}

// errCutShort: what was joined does not end where it should — a piece at the end is missing.
var errCutShort = errors.New("分卷不完整，可能缺少最后的分卷")

// extractJoined joins the pieces of a zip or a unitypackage into one file next to them, and unpacks that — a
// unitypackage goes into a folder of its own, as an archive program would put it.
func extractJoined(a archiveSet, what, pwd string) (string, error) {
	v := readVolSet(a)
	parent := filepath.Dir(a.Main)
	work, done := workDir(parent, core.SafeName(a.Name, 100), "joining")
	defer done()
	defer os.RemoveAll(work)
	if err := os.MkdirAll(work, 0755); err != nil {
		return "", fmt.Errorf("解压出错：无法创建文件夹（%v）", err)
	}
	ext := ".zip"
	if what == "unitypackage" {
		ext = ".unitypackage"
	}
	joined := filepath.Join(work, core.SafeName(a.Name, 100)+ext)
	if err := joinFiles(v, joined); err != nil {
		return "", fmt.Errorf("解压出错：无法合并分卷（%v）", err)
	}
	nums := v.numbers()
	if what == "unitypackage" {
		if !wholeGzip(joined) {
			return "", errors.New(cutShort(v, nums[len(nums)-1]))
		}
		out := filepath.Join(work, "out")
		if err := os.MkdirAll(out, 0755); err != nil {
			return "", err
		}
		if err := os.Rename(joined, filepath.Join(out, filepath.Base(joined))); err != nil {
			return "", err
		}
		return placeExtracted(out, parent, a.Name)
	}
	if !hasZipEnd(joined) {
		return "", errors.New(cutShort(v, nums[len(nums)-1]))
	}
	return extractInto(archiveSet{Main: joined, Parts: []string{joined}, Name: a.Name}, pwd, parent)
}

func joinFiles(v volSet, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	for _, n := range v.numbers() {
		in, err := os.Open(v.byNum[n])
		if err != nil {
			f.Close()
			return err
		}
		_, err = io.Copy(f, in)
		in.Close()
		if err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

// wholeGzip: the gzip stream reads to its end (one that stops short was cut: a piece is missing).
func wholeGzip(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return false
	}
	_, err = io.Copy(io.Discard, zr)
	return err == nil
}
