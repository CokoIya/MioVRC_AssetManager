package core

import (
	"strings"
	"sync/atomic"
	"time"
)

type SavedCookie struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Domain  string `json:"domain,omitempty"`
	Expires int64  `json:"expires,omitempty"` // unix seconds, 0 = until the browser closes
}

// DLRecord: a purchase file that has been downloaded (by downloadable id).
type DLRecord struct {
	Item string `json:"item"`
	Path string `json:"path"`
	At   int64  `json:"at"`
}

type BoothHit struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Shop     string  `json:"shop"`
	ShopSub  string  `json:"shopSub"`
	Thumb    string  `json:"thumb"`
	Price    string  `json:"price"`
	Category string  `json:"category"`
	Score    float64 `json:"score,omitempty"`
	Full     bool    `json:"full,omitempty"` // every distinctive word of the asset name is in it
}

type BoothMatch struct {
	ID    string     `json:"id,omitempty"` // picked automatically (unambiguous)
	Query string     `json:"query"`
	Hits  []BoothHit `json:"hits,omitempty"`
	Tried int64      `json:"tried"`
	Err   string     `json:"err,omitempty"`
}

// DefaultStyles: display name = Booth tags / words that put an outfit in it.
var DefaultStyles = []string{
	"JK=JK|制服|セーラー|セーラー服|ブレザー|学生服|学校|スクール|school|uniform|sailor",
	"Sexy=セクシー|sexy|バニー|bunny|ボンデージ|bondage|レオタード|leotard|ランジェリー|lingerie|下着|ハイレグ|性感",
	"H=R-18|R18|NSFW|18禁|成人向け",
	"可爱=かわいい|可愛い|カワイイ|キュート|cute|kawaii|ゆめかわ|ロリータ|lolita|フリル|甘ロリ",
	"女仆=メイド|maid",
	"成熟=大人|お姉さん|エレガント|elegant|上品|オフィス|スーツ|suit|秘書|OL",
	"泳装=水着|swimsuit|swimwear|bikini|ビキニ|スク水",
	"和风=和服|着物|浴衣|和風|kimono|yukata|巫女|袴",
	"中华=チャイナ|チャイナドレス|旗袍|qipao|漢服|汉服",
	"哥特=ゴシック|gothic|ゴスロリ|地雷|量産型|パンク|punk",
	"休闲=カジュアル|casual|パーカー|hoodie|ストリート|street|スポーツ|sports|ジャージ",
	"科幻=サイバー|cyber|サイバーパンク|cyberpunk|SF|メカ|mecha|テック|アーマー|armor|戦闘服",
	"睡衣=パジャマ|pajama|pyjama|ルームウェア|ナイトウェア|ネグリジェ",
}

const GumPrefix = "gr_"

func IsGumID(id string) bool { return strings.HasPrefix(id, GumPrefix) }

type PanFile struct {
	Name     string     `json:"n"`
	Size     int64      `json:"s,omitempty"`
	Dir      bool       `json:"d,omitempty"`
	Children []*PanFile `json:"c,omitempty"`
	Partial  bool       `json:"p,omitempty"` // folder not fully listed (limits)
}

type PanListing struct {
	Surl      string     `json:"surl"`
	Title     string     `json:"title"`
	Files     []*PanFile `json:"files"`
	Count     int        `json:"count"`
	Size      int64      `json:"size"`
	Fetched   int64      `json:"fetched"`
	Err       string     `json:"err,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
	// what changed at the last re-read that found a difference
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed int64    `json:"changed,omitempty"`
}

func IsPanShareKey(key string) bool {
	return strings.HasPrefix(key, "pan:") && !strings.Contains(key, "#")
}

type PurchaseOrder struct {
	ID   string `json:"id"`
	Date string `json:"date,omitempty"`
}

type Purchase struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Shop      string          `json:"shop,omitempty"`
	ShopURL   string          `json:"shopUrl,omitempty"`
	Thumb     string          `json:"thumb,omitempty"`
	Cover     string          `json:"cover,omitempty"` // local copy of the thumbnail
	Files     []string        `json:"files,omitempty"`
	Downloads []string        `json:"downloads,omitempty"`
	Gift      bool            `json:"gift,omitempty"`
	Orders    []PurchaseOrder `json:"orders,omitempty"`
	First     int64           `json:"first"`
	// a Gumroad purchase (the id starts with "gr_"): where its files are asked for, its pages, and why it
	// has no files when it has none
	Source  string   `json:"source,omitempty"`  // "" = Booth, "gumroad"
	DLPaths []string `json:"dlPaths,omitempty"` // next to Downloads
	DLPage  string   `json:"dlPage,omitempty"`  // the purchase's download page
	PageURL string   `json:"pageUrl,omitempty"` // the product's page
	Variant string   `json:"variant,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// When the purchase was made (latest order), falling back to when it was first synced.
func (p *Purchase) When() int64 {
	for _, o := range p.Orders {
		if t, err := time.ParseInLocation("2006-01-02", o.Date, time.Local); err == nil {
			return t.Unix()
		}
	}
	return p.First
}

var (
	PurchaseBusy atomic.Bool
)

type UpdateAsset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest,omitempty"` // "sha256:…" when GitHub provides it
}

type UpdateInfo struct {
	Version   string       `json:"version"`
	Tag       string       `json:"tag"`
	Name      string       `json:"name"`
	Notes     string       `json:"notes"`
	URL       string       `json:"url"` // release page
	Published string       `json:"published"`
	Zip       *UpdateAsset `json:"zip,omitempty"`
	Setup     *UpdateAsset `json:"setup,omitempty"`
}
