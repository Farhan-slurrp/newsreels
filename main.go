package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type Article struct {
	Title     string
	URL       string
	Thumbnail string
	Preview   string
}

var (
	articles     []Article
	currentPage  int = 1
	cacheUpdated bool
	cacheMutex   sync.RWMutex
)

const fallbackThumbnail = "https://upload.wikimedia.org/wikipedia/commons/thumb/b/b2/Y_Combinator_logo.svg/1200px-Y_Combinator_logo.svg.png"

func main() {
	cacheUpdated = false
	scrapeArticles(currentPage)

	go refreshCache()

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
	for {
		<-time.After(10 * time.Minute)

		cacheMutex.Lock()
		cacheUpdated = false
		articles = nil
		scrapeArticles(currentPage)
		cacheUpdated = true
		cacheMutex.Unlock()
	}
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

		if len(articles) >= 2 {
			cacheMutex.Lock()
			cacheUpdated = true
			cacheMutex.Unlock()
		}
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
	currentPage++

	newArticles := scrapeArticles(currentPage)

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(newArticles)
	if err != nil {
		http.Error(w, "Error returning articles", http.StatusInternalServerError)
	}
}

func serveTemplate(w http.ResponseWriter, r *http.Request) {
	cacheMutex.RLock()
	defer cacheMutex.RUnlock()

	if !cacheUpdated {
		http.Error(w, "Please wait while articles are being scraped...", http.StatusServiceUnavailable)
		return
	}

	tmpl, err := template.New("articles").Parse(`
		<!DOCTYPE html>
		<html lang="en">
		<head>
			<meta charset="UTF-8">
			<meta name="viewport" content="width=device-width, initial-scale=1.0">
			<title>Y Combinator Articles</title>
			<style>
				/* Fullscreen vertical scrolling */
				body {
					font-family: Arial, sans-serif;
					background-color: #f5f5f5;
					margin: 0;
					padding: 0;
					overflow: hidden;
				}
				.container {
					max-width: 100%;
					max-height: 100vh;
					overflow-y: scroll;
					scroll-snap-type: y mandatory;
					padding: 0;
					scroll-behavior: smooth;
				}
				.article {
					width: 100%;
					height: 100vh; /* Full screen height */
					display: flex;
					flex-direction: column;
					align-items: center;
					justify-content: center;
					background-color: black;
					scroll-snap-align: start;
					cursor: pointer;
					position: relative;
					overflow: hidden;
				}
				.article img {
					width: 100%;
					height: 100%;
					object-fit: cover;
					position: absolute;
					top: 0;
					left: 0;
				}
				.article .overlay {
					width: 100%;
					height: 100%;
					object-fit: cover;
					position: absolute;
					top: 0;
					left: 0;
					background-color: black;
					opacity: 70%;
					z-index: 1;
				}
				.article .texts {
					position: absolute;
					bottom: 40px;
					left: 20px;
					z-index: 2;
					display: flex;
					flex-direction: column;
					gap: 0.3em;
				}
				.texts .title {
					color: white;
					font-size: 1em;
					font-weight: bold;
					max-width: 80vw;
				}
				.texts .preview {
					color:rgb(194, 198, 199);
					font-size: 0.8em;
					max-width: 80vw;
					overflow: hidden;
					text-overflow: ellipsis;
				}
				.texts a {
					display: inline-block;
					margin-top: 10px;
					color:rgb(194, 198, 199);
					font-size: 0.8em;
					text-decoration: none;
				}
			</style>
		</head>
		<body>
			<div class="container" id="article-container">
				{{range .Articles}}
					<div class="article">
						<img src="{{.Thumbnail}}" alt="Thumbnail">
						<div class="overlay"></div>
						<div class="texts">
							<div class="title">
								{{.Title}}
							</div>
							<div class="preview">
								{{.Preview}}
							</div>
							<a href="{{.URL}}" target="_blank">Read More</a>
						</div>
					</div>
				{{end}}
			</div>
			<script>
				document.addEventListener('DOMContentLoaded', () => {
					let isFetching = false;
					const articleContainer = document.getElementById('article-container');

					function loadMoreArticles() {
						if (isFetching) return;
    					isFetching = true;
						console.log('Fetching new articles...');
						fetch('/load-more')
							.then(response => response.json())
							.then(data => {
								data.forEach(article => {
									const articleElement = document.createElement('div');
									articleElement.classList.add('article');

									const overlay = document.createElement('div');
									overlay.classList.add('overlay');
									articleElement.appendChild(overlay);

									const textsElement = document.createElement('div');
									textsElement.classList.add('texts');
									articleElement.appendChild(textsElement);

									const img = document.createElement('img');
									img.src = article.Thumbnail;
									img.alt = "Thumbnail";
									articleElement.appendChild(img);

									const titleDiv = document.createElement('div');
									titleDiv.classList.add('title');
									titleDiv.textContent = article.Title;
									textsElement.appendChild(titleDiv);

									const previewDiv = document.createElement('div');
									previewDiv.classList.add('preview');
									previewDiv.textContent = article.Preview;
									textsElement.appendChild(previewDiv);

									const readMoreLink = document.createElement('a');
									readMoreLink.href = article.URL;
									readMoreLink.target = "_blank";
									readMoreLink.textContent = "Read More";
									textsElement.appendChild(readMoreLink);

									articleContainer.appendChild(articleElement);
								});

							})
							.catch(error => {
								console.error('Error loading articles:', error);
							})
							.finally(() => {
								isFetching = false; // Reset the fetching flag
							});
					}

					articleContainer.addEventListener('scroll', () => {
						if (isFetching) {
							return;
						}
						const articles = Array.from(articleContainer.children);
						
						if (articles.length >= 30) {
							const lastFifteenArticles = articles.slice(-30);
							
							const lastArticle = lastFifteenArticles[0];

							const rect = lastArticle.getBoundingClientRect();
							
							if (rect.bottom <= window.innerHeight + 300) {
								console.log('Near end of page, triggering loadMoreArticles');
								loadMoreArticles();
							}
						}
					});
				});
			</script>
		</body>
		</html>
	`)
	if err != nil {
		http.Error(w, "Error creating template", http.StatusInternalServerError)
		return
	}

	data := struct {
		Articles     []Article
		CacheUpdated bool
	}{
		Articles:     articles,
		CacheUpdated: cacheUpdated,
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}
