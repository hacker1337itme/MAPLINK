package main

import (
    "crypto/md5"
    "crypto/sha256"
    "encoding/hex"
    "database/sql"
    "flag"
    "fmt"
    "io"
    "net/http"
    "os"
    "regexp"
    "strings"
    "bufio"
    _ "github.com/mattn/go-sqlite3"
)

// Fetch the HTML content of a webpage
func fetchHTML(url string) (string, error) {
    resp, err := http.Get(url)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("error: status code %d", resp.StatusCode)
    }

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", err
    }

    return string(body), nil
}

// Extract favicon links using improved regex
func extractFaviconLinks(content string) []string {
    var links []string
    re := regexp.MustCompile(`(?i)(https?://[^\"\s]*?/favicon\.ico|/favicon\.ico|favicon\.ico)`)
    matches := re.FindAllString(content, -1)

    // Deduplicate links using a map
    seen := map[string]struct{}{}
    for _, match := range matches {
        if _, ok := seen[match]; !ok {
            seen[match] = struct{}{}
            links = append(links, match)
        }
    }
    return links
}

// Calculate MD5 and SHA256 hashes of a file (favicon.ico)
func calculateHashes(url string) (string, string, error) {
    resp, err := http.Get(url)
    if err != nil {
        return "", "", err
    }
    defer resp.Body.Close()

    // Create a hash for MD5 and SHA256
    md5Hash := md5.New()
    sha256Hash := sha256.New()

    // Copy the response body to the hashes
    if _, err = io.Copy(md5Hash, resp.Body); err != nil {
        return "", "", err
    }

    // Re-fetch to read again for SHA256
    resp.Body.Close()
    resp, err = http.Get(url)
    if err != nil {
        return "", "", err
    }
    defer resp.Body.Close()

    if _, err = io.Copy(sha256Hash, resp.Body); err != nil {
        return "", "", err
    }

    return hex.EncodeToString(md5Hash.Sum(nil)), hex.EncodeToString(sha256Hash.Sum(nil)), nil
}

// Save hashes to SQLite database
func saveToDatabase(db *sql.DB, link, md5Hash, sha256Hash string) error {
    _, err := db.Exec("INSERT OR IGNORE INTO favicons(link, md5, sha256) VALUES(?, ?, ?)", link, md5Hash, sha256Hash)
    return err
}

// Resolve a relative link to an absolute URL
func resolveLink(baseURL, link string) string {
    if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
        return link
    }
    if link[0] == '/' {
        return baseURL + link
    }
    return baseURL + "/" + link
}

// Read URLs from a file
func readURLsFromFile(filename string) ([]string, error) {
    file, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    var urls []string
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        url := strings.TrimSpace(scanner.Text())
        if url != "" {
            urls = append(urls, url)
        }
    }

    if err := scanner.Err(); err != nil {
        return nil, err
    }

    return urls, nil
}

// Main function
func main() {
    // Command-line arguments
    var filename string
    flag.StringVar(&filename, "file", "", "File containing a list of URLs to scrape for favicon.ico links")
    flag.Parse()

    if filename == "" {
        fmt.Println("Please provide a filename using the -file flag.")
        return
    }

    // Read URLs from the file
    urls, err := readURLsFromFile(filename)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error reading URLs from file: %v\n", err)
        return
    }

    // Database setup
    db, err := sql.Open("sqlite3", "./favicons.db")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
        return
    }
    defer db.Close()

    // Create table if it doesn't exist
    sqlStmt := `
    CREATE TABLE IF NOT EXISTS favicons (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        link TEXT UNIQUE,
        md5 TEXT,
        sha256 TEXT
    );
    `
    if _, err := db.Exec(sqlStmt); err != nil {
        fmt.Fprintf(os.Stderr, "Error creating table: %v\n", err)
        return
    }

    // Process each URL
    for _, baseURL := range urls {
        fmt.Printf("Processing URL: %s\n", baseURL)

        // Fetch HTML
        htmlContent, err := fetchHTML(baseURL)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error fetching HTML: %v\n", err)
            continue
        }

        // Extract favicon links
        faviconLinks := extractFaviconLinks(htmlContent)
        if len(faviconLinks) == 0 {
            fmt.Println("No favicon.ico links found.")
            continue
        }

        // Check each favicon link and calculate hashes
        for _, link := range faviconLinks {
            fullURL := resolveLink(baseURL, link)

            md5Hash, sha256Hash, err := calculateHashes(fullURL)
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error calculating hash for %s: %v\n", fullURL, err)
                continue
            }
            fmt.Printf("Favicon: %s | MD5: %s | SHA256: %s\n", fullURL, md5Hash, sha256Hash)

            // Save to database
            err = saveToDatabase(db, fullURL, md5Hash, sha256Hash)
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error saving to database for %s: %v\n", link, err)
            }
        }
    }
}
