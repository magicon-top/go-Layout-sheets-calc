//go:build windows
//________________________________________________________
// Algoritm 1 (improved + optimized + external HTML). 
// Description: Go desktop application with resizable borderless rounded window via webview2
//________________________________________________________
package main

//________________________________________________________
import (
	"container/heap"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	webview "github.com/jchv/go-webview2"
)

//________________________________________________________
const serverPort = ":8123"

// Глобальная переменная для HTML (загружается из файла при старте)
var indexHTML string

//________________________________________________________
type OrderItem struct {
	PageNum  int
	Quantity int
}

type CalcRequest struct {
	OvershootPct float64 `json:"overshoot"`
	Capacity     int     `json:"capacity"`
	Orders       string  `json:"orders"`
}

type ItemReport struct {
	PageNum   int     `json:"page_num"`
	Target    int     `json:"target"`
	Produced  int     `json:"produced"`
	Overshoot float64 `json:"overshoot"`
	SlotsStr  string  `json:"slots_str"`
	SlotsList []int   `json:"slots_list"`
}

type CalcResponse struct {
	Success        bool         `json:"success"`
	Message        string       `json:"message"`
	TotalSheets    int          `json:"total_sheets"`
	TotalForms     int          `json:"total_forms"`
	Forms          []int        `json:"forms"`
	ItemReports    []ItemReport `json:"item_reports"`
	TotalOrdered   int          `json:"total_ordered"`
	TotalProduced  int          `json:"total_produced"`
	TotalOvershoot float64      `json:"total_overshoot"`
	PrintCodes     []string     `json:"print_codes"`
	FormNames      []string     `json:"form_names"`
}

type FormBlock struct {
	FormName     string `json:"form_name"`
	FormNameHtml string `json:"form_name_html"`
	CodeLine     string `json:"code_line"`
}

type ExtendedCalcResponse struct {
	CalcResponse
	FormBlocks []FormBlock `json:"form_blocks"`
	Logs       string      `json:"logs"`
}

type FindMinOvershootRequest struct {
	Capacity         int     `json:"capacity"`
	Orders           string  `json:"orders"`
	CurrentOvershoot float64 `json:"current_overshoot"`
	StartForms       int     `json:"start_forms"`
}

type FindMinOvershootResponse struct {
	Success   bool    `json:"success"`
	Overshoot float64 `json:"overshoot"`
	Message   string  `json:"message"`
}

//________________________________________________________
// Priority queue for D'Hondt allocation
type dhondtItem struct {
	index int
	score float64
	slots int
}
type dhondtPQ []*dhondtItem

func (pq dhondtPQ) Len() int           { return len(pq) }
func (pq dhondtPQ) Less(i, j int) bool { return pq[i].score > pq[j].score }
func (pq dhondtPQ) Swap(i, j int)      { pq[i], pq[j] = pq[j], pq[i] }
func (pq *dhondtPQ) Push(x any)        { *pq = append(*pq, x.(*dhondtItem)) }
func (pq *dhondtPQ) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

//________________________________________________________
// Global cache
var calcCache = struct {
	sync.RWMutex
	m map[string]ExtendedCalcResponse
}{m: make(map[string]ExtendedCalcResponse)}

//________________________________________________________
// Helper: decode JSON from request
func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

//________________________________________________________
// Icon setter (unchanged)
func setIconFromPNGFile(hwnd uintptr, pngPath string) {
	if runtime.GOOS != "windows" {
		return
	}
	pngData, err := os.ReadFile(pngPath)
	if err != nil {
		return
	}
	size := len(pngData)
	header := []byte{0, 0, 1, 0, 1, 0, 0, 0, 0, 0, 1, 0, 32, 0, byte(size), byte(size >> 8), byte(size >> 16), byte(size >> 24), 22, 0, 0, 0}
	icoPath := filepath.Join(os.TempDir(), fmt.Sprintf("temp_logo_%d.ico", time.Now().UnixNano()))
	if err := os.WriteFile(icoPath, append(header, pngData...), 0644); err != nil {
		return
	}
	defer os.Remove(icoPath)

	user32 := syscall.NewLazyDLL("user32.dll")
	loadImage := user32.NewProc("LoadImageW")
	sendMessage := user32.NewProc("SendMessageW")
	pathPtr, _ := syscall.UTF16PtrFromString(icoPath)
	hIcon, _, _ := loadImage.Call(0, uintptr(unsafe.Pointer(pathPtr)), 1, 0, 0, 0x00000010)
	if hIcon == 0 {
		return
	}
	const WM_SETICON = 0x0080
	sendMessage.Call(hwnd, WM_SETICON, 0, hIcon)
	sendMessage.Call(hwnd, WM_SETICON, 1, hIcon)
}

//________________________________________________________
// Window setup functions (unchanged)
func setupFramelessWindow(w webview.WebView) {
	if runtime.GOOS != "windows" {
		return
	}
	hwnd := uintptr(w.Window())
	user32 := syscall.NewLazyDLL("user32.dll")
	setWindowLongPtr := user32.NewProc("SetWindowLongPtrW")
	if setWindowLongPtr.Find() != nil {
		setWindowLongPtr = user32.NewProc("SetWindowLongA")
	}
	getWindowLongPtr := user32.NewProc("GetWindowLongPtrW")
	if getWindowLongPtr.Find() != nil {
		getWindowLongPtr = user32.NewProc("GetWindowLongA")
	}
	const GWL_STYLE = 0xFFFFFFF0
	const WS_POPUP = 0x80000000
	const WS_THICKFRAME = 0x00040000
	const WS_SYSMENU = 0x00080000
	const WS_MAXIMIZEBOX = 0x00010000
	const WS_MINIMIZEBOX = 0x00020000

	style, _, _ := getWindowLongPtr.Call(hwnd, uintptr(GWL_STYLE))
	newStyle := (style &^ 0x00C00000) | WS_POPUP | WS_THICKFRAME | WS_SYSMENU | WS_MAXIMIZEBOX | WS_MINIMIZEBOX
	setWindowLongPtr.Call(hwnd, uintptr(GWL_STYLE), newStyle)
	updateRoundedRegion(w)
}

func updateRoundedRegion(w webview.WebView) {
	if runtime.GOOS != "windows" {
		return
	}
	hwnd := uintptr(w.Window())
	user32 := syscall.NewLazyDLL("user32.dll")
	gdi32 := syscall.NewLazyDLL("gdi32.dll")
	createRoundRectRgn := gdi32.NewProc("CreateRoundRectRgn")
	setWindowRgn := user32.NewProc("SetWindowRgn")
	getWindowRect := user32.NewProc("GetWindowRect")
	type rect struct{ Left, Top, Right, Bottom int32 }
	var rObj rect
	getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rObj)))
	width := int(rObj.Right - rObj.Left)
	height := int(rObj.Bottom - rObj.Top)
	hrgn, _, _ := createRoundRectRgn.Call(0, 0, uintptr(width), uintptr(height), 16, 16)
	if hrgn != 0 {
		setWindowRgn.Call(hwnd, hrgn, 1)
	}
}

func dragWindow(w webview.WebView) {
	if runtime.GOOS != "windows" {
		return
	}
	hwnd := uintptr(w.Window())
	user32 := syscall.NewLazyDLL("user32.dll")
	releaseCapture := user32.NewProc("ReleaseCapture")
	sendMessage := user32.NewProc("SendMessageW")
	const WM_NCLBUTTONDOWN = 0x00A1
	const HTCAPTION = 2
	releaseCapture.Call()
	sendMessage.Call(hwnd, WM_NCLBUTTONDOWN, uintptr(HTCAPTION), 0)
}

//________________________________________________________
// Main
func main() {
	execDir, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot determine executable path:", err)
		os.Exit(1)
	}
	baseDir := filepath.Dir(execDir)

	// Загрузка HTML из папки settings
	htmlPath := filepath.Join(baseDir, "settings", "index.html")
	htmlData, err := os.ReadFile(htmlPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read index.html from %s: %v\n", htmlPath, err)
		fmt.Fprintln(os.Stderr, "Please ensure settings/index.html exists next to the executable.")
		os.Exit(1)
	}
	indexHTML = string(htmlData)

	// Запуск splash-заставки, если есть
	splashPath := filepath.Join(baseDir, "settings", "splash.exe")
	if _, err := os.Stat(splashPath); err == nil {
		cmd := exec.Command(splashPath, "500")
		cmd.Dir = filepath.Join(baseDir, "settings")
		_ = cmd.Start()
	}

	// HTTP-сервер
	go func() {
		http.HandleFunc("/", serveUI)
		http.HandleFunc("/api/calculate", handleCalc)
		http.HandleFunc("/api/find_min_overshoot", handleFindMinOvershoot)
		http.HandleFunc("/api/close", handleClose)
		_ = http.ListenAndServe(serverPort, nil)
	}()

	time.Sleep(200 * time.Millisecond)
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("Layout-sheets-calc")
	setupFramelessWindow(w)

	// Установка размера окна: Y=0, высота 95% экрана, X сохраняется
	if runtime.GOOS == "windows" {
		user32 := syscall.NewLazyDLL("user32.dll")
		getSystemMetrics := user32.NewProc("GetSystemMetrics")
		screenHeight, _, _ := getSystemMetrics.Call(1)
		if screenHeight > 0 {
			newHeight := int(float64(screenHeight) * 0.95)
			getWindowRect := user32.NewProc("GetWindowRect")
			var rect struct{ Left, Top, Right, Bottom int32 }
			hwnd := uintptr(w.Window())
			getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
			currentX := rect.Left
			currentWidth := rect.Right - rect.Left
			setWindowPos := user32.NewProc("SetWindowPos")
			const SWP_NOZORDER = 0x0004
			setWindowPos.Call(hwnd, 0, uintptr(currentX), uintptr(0), uintptr(currentWidth), uintptr(newHeight), uintptr(SWP_NOZORDER))
			updateRoundedRegion(w)
		}
	}

	// Установка иконки
	logoPath := filepath.Join(baseDir, "settings", "logo.png")
	setIconFromPNGFile(uintptr(w.Window()), logoPath)

	w.Bind("startDrag", func() { dragWindow(w) })
	w.Bind("updateWindowRegion", func() { updateRoundedRegion(w) })
	w.Bind("closeAppNative", func() {
		w.Destroy()
		os.Exit(0)
	})
	w.Navigate("http://localhost" + serverPort)
	w.Run()
}

//________________________________________________________
// HTTP handlers
func serveUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

func handleClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success":true}`))
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}()
}

//________________________________________________________
// Calculation core
func calculateHeuristic(items []OrderItem, capacity int, maxOvr float64) ([][]int, []int, []string) {
	var logs []string
	M := len(items)
	produced := make([]int, M)
	var forms [][]int
	var runs []int

	maxAllowed := make([]int, M)
	for i, it := range items {
		maxAllowed[i] = int(math.Floor(float64(it.Quantity) * (1.0 + maxOvr/100.0)))
	}

	remaining := make([]int, 0, M)
	iteration := 1

	for {
		remaining = remaining[:0]
		for i := 0; i < M; i++ {
			if produced[i] < items[i].Quantity {
				remaining = append(remaining, i)
			}
		}
		if len(remaining) == 0 {
			logs = append(logs, fmt.Sprintf("\nDONE: All %d items placed.", M))
			break
		}
		if iteration > 1000 {
			logs = append(logs, "\nSAFETY STOP: 1000 cycles.")
			break
		}

		logs = append(logs, fmt.Sprintf("\n--- CYCLE %d ---", iteration))
		logs = append(logs, fmt.Sprintf("Items left: %d", len(remaining)))

		slots := make([]int, M)
		unallocated := capacity
		pq := make(dhondtPQ, 0, len(remaining))

		if unallocated >= len(remaining) {
			for _, i := range remaining {
				slots[i] = 1
				unallocated--
			}
			logs = append(logs, "Pre-allocated 1 slot per item.")
		} else {
			logs = append(logs, "Warning: capacity < items left.")
		}

		for _, i := range remaining {
			need := items[i].Quantity - produced[i]
			maxRemainingOvr := maxAllowed[i] - produced[i]
			if slots[i]+1 <= maxRemainingOvr {
				score := float64(need) / float64(slots[i]+1)
				pq = append(pq, &dhondtItem{index: i, score: score, slots: slots[i]})
			}
		}
		heap.Init(&pq)

		for unallocated > 0 && pq.Len() > 0 {
			top := heap.Pop(&pq).(*dhondtItem)
			top.slots++
			slots[top.index]++
			unallocated--

			maxRemainingOvr := maxAllowed[top.index] - produced[top.index]
			if top.slots+1 <= maxRemainingOvr {
				need := items[top.index].Quantity - produced[top.index]
				top.score = float64(need) / float64(top.slots+1)
				heap.Push(&pq, top)
			}
		}
		if unallocated > 0 {
			logs = append(logs, "Stopped allocation: overshoot constraints.")
		}

		var allocLog strings.Builder
		for i, s := range slots {
			if s > 0 {
				need := items[i].Quantity - produced[i]
				allocLog.WriteString(fmt.Sprintf("[Pg %d : need %d → %d slots] ", items[i].PageNum, need, s))
			}
		}
		logs = append(logs, fmt.Sprintf("Slot distribution (%d places):", capacity))
		logs = append(logs, "  "+allocLog.String())

		rLowerBound, rUpperBound := 0, math.MaxInt32
		for i, s := range slots {
			if s > 0 {
				need := items[i].Quantity - produced[i]
				reqMin := (need + s - 1) / s
				if reqMin > rLowerBound {
					rLowerBound = reqMin
				}
				maxRemainingOvr := maxAllowed[i] - produced[i]
				maxAllowedR := maxRemainingOvr / s
				if maxAllowedR < rUpperBound {
					rUpperBound = maxAllowedR
				}
			}
		}
		R := 1
		if rLowerBound <= rUpperBound {
			R = rLowerBound
			logs = append(logs, fmt.Sprintf("Can complete all, R = %d", R))
		} else {
			R = rUpperBound
			if R <= 0 {
				R = 1
			}
			logs = append(logs, fmt.Sprintf("Bottleneck limits R to %d", R))
		}

		for i, s := range slots {
			if s > 0 {
				produced[i] += R * s
			}
		}
		forms = append(forms, slots)
		runs = append(runs, R)
		iteration++
	}

	layouts := make([][]int, M)
	for i := 0; i < M; i++ {
		layouts[i] = make([]int, len(forms))
		for j := 0; j < len(forms); j++ {
			layouts[i][j] = forms[j][i]
		}
	}
	return layouts, runs, logs
}

func calculateCore(req CalcRequest) (ExtendedCalcResponse, error) {
	parts := strings.Fields(req.Orders)
	var items []OrderItem
	var orders []int
	for _, p := range parts {
		sub := strings.Split(p, "*")
		if len(sub) == 2 {
			pageNum, e1 := strconv.Atoi(sub[0])
			qty, e2 := strconv.Atoi(sub[1])
			if e1 == nil && e2 == nil && qty > 0 {
				items = append(items, OrderItem{PageNum: pageNum, Quantity: qty})
				orders = append(orders, qty)
			}
		}
	}
	if len(orders) == 0 {
		return ExtendedCalcResponse{}, fmt.Errorf("No valid orders")
	}
	layouts, runs, logs := calculateHeuristic(items, req.Capacity, req.OvershootPct)
	if len(runs) == 0 {
		return ExtendedCalcResponse{}, fmt.Errorf("No layout found")
	}
	resp := buildResponse(runs, layouts, items, req.Capacity)
	resp.TotalOvershoot = ((float64(resp.TotalProduced) - float64(resp.TotalOrdered)) / float64(resp.TotalOrdered)) * 100.0
	resp.Logs = strings.Join(logs, "\n")
	return resp, nil
}

func buildResponse(R []int, layouts [][]int, items []OrderItem, capacity int) ExtendedCalcResponse {
	totalSheets := 0
	for _, r := range R {
		totalSheets += r
	}
	var itemReports []ItemReport
	totalOrdered, totalProduced := 0, 0
	var formBlocks []FormBlock

	for j := 0; j < len(R); j++ {
		fName := fmt.Sprintf("Sheet %d %d", j+1, R[j])
		fNameHtml := fmt.Sprintf("Sheet %d <span class=\"sheet-badge\">%d</span> Pcs", j+1, R[j])
		var b strings.Builder
		for i := 0; i < len(items); i++ {
			if layouts[i][j] > 0 {
				b.WriteString(fmt.Sprintf("%d*%d ", items[i].PageNum, layouts[i][j]))
			}
		}
		line := strings.TrimSpace(b.String())
		formBlocks = append(formBlocks, FormBlock{
			FormName:     fName,
			FormNameHtml: fNameHtml,
			CodeLine:     line,
		})
	}

	for i, item := range items {
		produced := 0
		var slotsList []int
		for j := 0; j < len(R); j++ {
			slots := layouts[i][j]
			produced += slots * R[j]
			slotsList = append(slotsList, slots)
		}
		totalOrdered += item.Quantity
		totalProduced += produced
		overshootPct := ((float64(produced) - float64(item.Quantity)) / float64(item.Quantity)) * 100.0
		itemReports = append(itemReports, ItemReport{
			PageNum:   item.PageNum,
			Target:    item.Quantity,
			Produced:  produced,
			Overshoot: overshootPct,
			SlotsStr:  strings.Trim(strings.Join(strings.Fields(fmt.Sprint(slotsList)), " | "), "[]"),
			SlotsList: slotsList,
		})
	}
	globalOvershoot := ((float64(totalProduced) - float64(totalOrdered)) / float64(totalOrdered)) * 100.0
	base := CalcResponse{
		Success:        true,
		TotalSheets:    totalSheets,
		TotalForms:     len(R),
		Forms:          R,
		ItemReports:    itemReports,
		TotalOrdered:   totalOrdered,
		TotalProduced:  totalProduced,
		TotalOvershoot: globalOvershoot,
	}
	return ExtendedCalcResponse{
		CalcResponse: base,
		FormBlocks:   formBlocks,
	}
}

//________________________________________________________
// HTTP API handlers
func handleCalc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CalcRequest
	if err := decodeJSON(r, &req); err != nil {
		sendJSON(w, CalcResponse{Success: false, Message: "Invalid JSON"})
		return
	}
	cacheKey := fmt.Sprintf("%f|%d|%s", req.OvershootPct, req.Capacity, req.Orders)
	calcCache.RLock()
	cached, found := calcCache.m[cacheKey]
	calcCache.RUnlock()
	if found {
		sendJSON(w, cached)
		return
	}
	resp, err := calculateCore(req)
	if err != nil {
		sendJSON(w, ExtendedCalcResponse{
			CalcResponse: CalcResponse{Success: false, Message: err.Error()},
		})
		return
	}
	calcCache.Lock()
	calcCache.m[cacheKey] = resp
	calcCache.Unlock()
	sendJSON(w, resp)
}

func handleFindMinOvershoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req FindMinOvershootRequest
	if err := decodeJSON(r, &req); err != nil {
		sendJSON(w, FindMinOvershootResponse{Success: false, Message: "Invalid JSON"})
		return
	}
	parts := strings.Fields(req.Orders)
	minSheets := int(math.Ceil(float64(len(parts)) / float64(req.Capacity)))
	if req.StartForms <= minSheets {
		sendJSON(w, FindMinOvershootResponse{Success: false, Message: "Already at minimum"})
		return
	}
	low, high := int(req.CurrentOvershoot)+1, 10000
	best := -1
	for low <= high {
		mid := (low + high) / 2
		calcReq := CalcRequest{
			OvershootPct: float64(mid),
			Capacity:     req.Capacity,
			Orders:       req.Orders,
		}
		resp, err := calculateCore(calcReq)
		if err != nil {
			sendJSON(w, FindMinOvershootResponse{Success: false, Message: err.Error()})
			return
		}
		if resp.TotalForms < req.StartForms {
			best = mid
			high = mid - 1
		} else {
			low = mid + 1
		}
	}
	if best == -1 {
		sendJSON(w, FindMinOvershootResponse{Success: false, Message: "Cannot reduce further"})
		return
	}
	sendJSON(w, FindMinOvershootResponse{Success: true, Overshoot: float64(best)})
}

func sendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}