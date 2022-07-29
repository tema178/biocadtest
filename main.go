package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// Время выделенное для записи данных клиенту
	writeWait = 10 * time.Second
	// Период ожидания Pong
	pongWait = 60 * time.Second
	// Период отправки Ping
	pingPeriod = (pongWait * 9) / 10
	// Частота проверки изменений на сервере
	filePeriod = 10 * time.Second
)

type plotData struct{ //структура для построения графика
	PlotTitle string
	Datas [] Data
}

type Data struct {
	Time float64
	Value float64
}
var (
	b        = false
	pldata   = plotData{}
	DirName  string
	t        string
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

func readFileIfModified(lastMod time.Time, b bool) ([] byte, time.Time, error) {
	//чтение информации о каталоге
	fi, err := os.Stat(DirName)
	if err != nil {
		return nil, lastMod, err
	}
	//сравнение времени последнего изменения каталога
	if !fi.ModTime().After(lastMod) && b != true {
		return nil, lastMod, nil
	}
	//чтение всех файлов в каталоге
	s := ""
	p, err := ioutil.ReadDir(DirName +"/")
	if err != nil {
		return nil, fi.ModTime(), err
	}
	//отбор файлов с расширрением .txt
	for _, file := range p{
		if strings.HasSuffix(file.Name(), ".txt") {
			s = s + file.Name() + ":"
		}
	}
	s = strings.TrimSuffix(s,":")
	//проверка измения файлов .txt
	if t == s && b!= true {
		return nil, fi.ModTime(), nil
	}
	t = s
	return []byte(t), fi.ModTime(), nil
}
func reader(ws *websocket.Conn) {
	defer ws.Close()
	ws.SetReadLimit(512)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error { ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			break
		}
	}
}
func writer(ws *websocket.Conn, lastMod time.Time) {
	lastError := ""
	pingTicker := time.NewTicker(pingPeriod)
	fileTicker := time.NewTicker(filePeriod)
	defer func() {
		pingTicker.Stop()
		fileTicker.Stop()
		ws.Close()
	}()
	for {
		select {
		case <-fileTicker.C: //обработка тикета по проверки изменений на сервере
			var p []byte
			var err error
			b = false
			p, lastMod, err = readFileIfModified(lastMod, b)
			if err != nil {
				if s := err.Error(); s != lastError {
					lastError = s
					p = []byte(lastError)
				}
			} else {
				lastError = ""
			}

			if p != nil {
				ws.SetWriteDeadline(time.Now().Add(writeWait))
				if err := ws.WriteMessage(websocket.TextMessage, p); err != nil {
					return
				}
			}
		case <-pingTicker.C: //отправка Ping'а
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := ws.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}
func serveWs(res http.ResponseWriter, req *http.Request) {
	ws, err := upgrader.Upgrade(res, req, nil)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			log.Println(err)
		}
		return
	}

	var lastMod time.Time
	if n, err := strconv.ParseInt(req.FormValue("lastMod"), 16, 64); err == nil {
		lastMod = time.Unix(0, n)
	}

	go writer(ws, lastMod)
	reader(ws)
}
func index(res http.ResponseWriter, req *http.Request) {
	b = true
	p, lastMod, err := readFileIfModified(time.Time{}, b)
	if err != nil {
		p = []byte(err.Error())
		lastMod = time.Unix(0, 0)
	}
	s := strings.Split(string(p),":")

	var v = struct {
		Host    string
		Data    [] string
		LastMod string
	}{
		req.Host,
		s,
		strconv.FormatInt(lastMod.UnixNano(), 16),
	}

	t, err:= template.ParseFiles("upload.html")
	if err != nil {
		fmt.Fprintf(res, err.Error())}

	t.ExecuteTemplate(res, "upload", &v )
}


func determineListenAddress() (string, error) {
	port := os.Getenv("PORT")
	if port == "" {
		return "", fmt.Errorf("$PORT not set")
	}
	return ":" + port, nil
}
func xyOut (name string) [] Data {
	var Datas []  Data
	var er Data
	i := 6 //6 - для откидывания заголовка
	//чтение файла и вывод данных в числовой массив
	b, err := ioutil.ReadFile("files/"+name)
	if err != nil {
		panic(err)
	}
	//замена всех спец. символом пробелами
	s := strings.ReplaceAll( string(b) ,"\r", " ")
	s = strings.ReplaceAll( s,"\n", " ")
	s = strings.ReplaceAll( s,"\t", " ")
	for strings.Contains(s, "  ") == true {s = strings.ReplaceAll( s,"  ", " ")	}
	s = strings.ReplaceAll( s," ", "\r")
	//замена суффикса, если он есть
	s = strings.TrimSuffix(s, "\r")
	r := strings.Split(s,"\r")
	// разделение на метку и значение
	for i < len(r){
		d, _ := strconv.ParseFloat(r[i], 64)
		if i%2 == 0	{ er.Time = d
		}else {er.Value = d
			Datas = append(Datas,er)}
		i++
	}

	return Datas
}
func delFile (name string) {
	err := os.Remove("files/" + name)
	if err != nil {
		fmt.Println(err)
		return
	}
}
func plot (res http.ResponseWriter, req *http.Request) {
	t, _:= template.ParseFiles("plot.html")
	t.ExecuteTemplate(res, "plot", pldata)
}
func processing (res http.ResponseWriter, req *http.Request) {

	if req.FormValue("mPlot") == "Построить график"{
		//data := plotData{}
		pldata.PlotTitle = req.FormValue("filelist")
		pldata.Datas = xyOut(req.FormValue("filelist"))

		http.Redirect(res, req, "/plot", 302)
	}

//Функция удаления
	if req.FormValue("del") =="Удалить"{
		delFile(req.FormValue("filelist"))
		http.Redirect(res, req, "/", 302)
	}

}
func upload(res http.ResponseWriter, req *http.Request)  {
	//Загрузка файлов
	if req.PostFormValue("upl") =="Загрузить" {

		src, hdr, err := req.FormFile("my-file")
		if err != nil {
			http.Redirect(res, req, "/", 302)
			http.Error(res, err.Error(), 500)
			return

		}
		defer src.Close()

		//куда записать файл
		dst, err := os.Create(filepath.Join("./files/", hdr.Filename))
		if err != nil {
			http.Error(res, err.Error(), 500)
			return
		}
		defer dst.Close()
		io.Copy(dst, src)
		http.Redirect(res, req, "/", 302)
	}
}

func main() {
	addr, _ := determineListenAddress()
	DirName = "files"
	http.HandleFunc("/upload", upload)
	http.HandleFunc("/plot", plot)
	http.HandleFunc("/processing", processing)
	http.HandleFunc("/ws", serveWs)
	http.HandleFunc("/", index)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}