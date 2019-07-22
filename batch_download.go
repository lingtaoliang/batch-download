package main

import (
  "encoding/json"
  "fmt"
  "io"
  "io/ioutil"
  "log"
  "net/http"
  "os"
  "regexp"
  "strings"
  "sync"
)

type DownloadInfo struct {
  Url  string `json:"url"`
  Path string `json:"path"`
}

const (
  parallel = 5
)

var logger *log.Logger
var file *os.File
var err error

func init() {
  file, err = os.OpenFile("daily.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0664)
  if err != nil {
    logger.Fatal(err)
  }
  logger = log.New(file, "", log.LstdFlags)
  logger.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
  defer file.Close()
  manifest, err := loadDownloadInfo();
  if err != nil {
    logger.Panic(err)
    return
  }
  var httpClient = &http.Client{}
  failedChannel := make(chan DownloadInfo)
  successChannel := make(chan DownloadInfo)
  msgChannel := make(chan string)
  w := sync.WaitGroup{}
  report := sync.WaitGroup{}
  go successReport(&report, successChannel, msgChannel)
  go failedReport(&report, failedChannel, msgChannel)
  report.Add(2)
  go msgReport(msgChannel)
  for index, downloadInfo := range *manifest {
    if index%parallel == 0 {
      w.Wait()
    }
    w.Add(1)
    go downloadToFile(&w, httpClient, downloadInfo, failedChannel, successChannel)
  }
  w.Wait()
  close(successChannel)
  close(failedChannel)
  report.Wait()
  msgChannel <- "批量下载处理完毕!"
  close(msgChannel)
  fmt.Scanln()
}

func successReport(report *sync.WaitGroup, successChannel chan DownloadInfo, msgChan chan string) {
  defer report.Done()
  for downloadInfo := range successChannel {
    msgChan <- "/" + downloadInfo.Path + " 下载成功"
  }
}

func failedReport(report *sync.WaitGroup, failedChan chan DownloadInfo, msgChan chan string) {
  defer report.Done()
  failedList := []DownloadInfo{}
  for downloadInfo := range failedChan {
    msgChan <- "/" + downloadInfo.Path + " 下载失败"
    failedList = append(failedList, downloadInfo)
  }
  if len(failedList) > 0 {
    failedBytes, err := json.Marshal(failedList)
    if err != nil {
      logger.Println("to json failed", err)
    } else {
      ioutil.WriteFile("failed.json", failedBytes, 0664)
    }
  }
}

func msgReport(msgChan chan string) {
  for msg := range msgChan {
    fmt.Println(msg)
  }
}

func loadDownloadInfo() (*[]DownloadInfo, error) {
  bytes, err := ioutil.ReadFile("manifest.json")
  if err != nil {
    return nil, err
  }
  var manifest []DownloadInfo
  err = json.Unmarshal(bytes, &manifest)
  if err != nil {
    return nil, err
  }
  return &manifest, nil;
}

func downloadToFile(w *sync.WaitGroup, httpClient *http.Client, downloadInfo DownloadInfo, failedChannel chan DownloadInfo, successChannel chan DownloadInfo) {
  defer w.Done()
  exp, err := regexp.Compile("[^/\\\\]+")
  if err != nil {
    logger.Panic(err)
  }
  path := downloadInfo.Path
  url := downloadInfo.Url
  paths := exp.FindAllString(path, -1)
  path = strings.Join(paths, "/")
  if isExist(path) {
    successChannel <- downloadInfo
    return
  }
  req, err := http.NewRequest("GET", url, nil);
  if err != nil {
    failedChannel <- downloadInfo
    logger.Println(url, path, err)
    return
  }
  response, err := httpClient.Do(req)
  if err != nil {
    failedChannel <- downloadInfo
    logger.Printf(url, path, err)
    return
  }
  defer response.Body.Close()
  if response.StatusCode != 200 {
    failedChannel <- downloadInfo
    logger.Printf("request failed", url, path, response.StatusCode)
    return
  }
  if paths == nil {
    failedChannel <- downloadInfo
    logger.Println("path is empty. ", url, path)
    return
  }
  if len(paths) > 1 {
    dirPath := strings.Join(paths[0:len(paths)-1], "/")
    os.MkdirAll(dirPath, 0774)
  }
  writePath := path + ".bak"
  file, err := os.OpenFile(writePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
  if err != nil {
    failedChannel <- downloadInfo
    logger.Println("open file failed.", url, path, err)
    return
  }
  defer file.Close()
  _, err = io.Copy(file, response.Body)
  if err != nil {
    failedChannel <- downloadInfo
    logger.Println("write file failed.", url, path, err)
    return
  }
  file.Close()
  err = os.Rename(writePath, path)
  if err != nil {
    failedChannel <- downloadInfo
    logger.Println("rename file failed.", url, path, err)
    return
  }
  successChannel <- downloadInfo
}

func isExist(path string) bool {
  _, err := os.Stat(path)
  return err == nil || os.IsExist(err)
}