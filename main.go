package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Article struct {
	Title     string
	URL       string
	Thumbnail string
	Preview   string
}

var (
	articles    []Article
	currentPage int = 1
)

const fallbackThumbnail = "https://upload.wikimedia.org/wikipedia/commons/thumb/b/b2/Y_Combinator_logo.svg/1200px-Y_Combinator_logo.svg.png"

func main() {
	go refreshCache()

	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static", fs))
	http.HandleFunc("/", serveTemplate)
	http.HandleFunc("/load-more", loadMoreArticles)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Println("Server started on http://localhost:" + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func refreshCache() {
	articles = nil
	scrapeArticles(currentPage)
}

func scrapeArticles(page int) []Article {
	url := fmt.Sprintf("https://news.ycombinator.com/news?p=%d", page)

	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Error fetching the page: %v", err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Fatalf("Error parsing the page: %v", err)
	}

	tempArticles := make([]Article, 0)
	doc.Find("tr.athing").Each(func(index int, item *goquery.Selection) {
		title := item.Find("td:nth-child(3) > span > a").Text()
		link, _ := item.Find("td:nth-child(3) > span > a").Attr("href")

		if !strings.HasPrefix(link, "http") {
			link = "https://news.ycombinator.com/" + link
		}

		thumbnail := scrapeThumbnailFromArticle(link)

		if thumbnail == "" {
			thumbnail = fallbackThumbnail
		}

		preview := scrapePreviewFromArticle(link)

		article := Article{
			Title:     title,
			URL:       link,
			Thumbnail: thumbnail,
			Preview:   preview,
		}
		articles = append(articles, article)
		tempArticles = append(tempArticles, article)
	})

	return tempArticles
}

func scrapeThumbnailFromArticle(articleURL string) string {
	resp, err := http.Get(articleURL)
	if err != nil {
		log.Printf("Failed to fetch article %s: %v\n", articleURL, err)
		return ""
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Printf("Failed to parse article %s: %v\n", articleURL, err)
		return ""
	}

	var thumbnail string
	doc.Find("meta[property='og:image']").Each(func(index int, meta *goquery.Selection) {
		thumbnail, _ = meta.Attr("content")
		if thumbnail != "" {
			return
		}
	})

	if thumbnail == "" {
		doc.Find("img").First().Each(func(index int, img *goquery.Selection) {
			thumbnail, _ = img.Attr("src")
			if !strings.HasPrefix(thumbnail, "http") {
				thumbnail = articleURL + thumbnail
			}
		})
	}

	return thumbnail
}

func scrapePreviewFromArticle(articleURL string) string {
	resp, err := http.Get(articleURL)
	if err != nil {
		log.Printf("Failed to fetch article %s: %v\n", articleURL, err)
		return "No preview available"
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Printf("Failed to parse article %s: %v\n", articleURL, err)
		return "No preview available"
	}

	preview := ""
	doc.Find("meta[name='description']").Each(func(index int, meta *goquery.Selection) {
		preview, _ = meta.Attr("content")
	})

	if preview == "" {
		doc.Find("body").Each(func(index int, body *goquery.Selection) {
			preview = body.Text()
		})
	}

	words := strings.Fields(preview)
	if len(words) > 40 {
		preview = strings.Join(words[:40], " ")
	}

	if len(preview) > 300 {
		preview = preview[:300]
	}

	return preview + "..."
}

func loadMoreArticles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset > 0 {
		err := json.NewEncoder(w).Encode(articles[offset:])
		if err != nil {
			http.Error(w, "Error returning articles", http.StatusInternalServerError)
		}
		return
	}
	currentPage++

	newArticles := scrapeArticles(currentPage)

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(newArticles)
	if err != nil {
		http.Error(w, "Error returning articles", http.StatusInternalServerError)
	}
}

func serveTemplate(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, "Error creating template", http.StatusInternalServerError)
		return
	}

	data := struct {
		Articles []Article
	}{
		Articles: articles,
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error rendering template: %v", err), http.StatusInternalServerError)
		return
	}
}
