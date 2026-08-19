//go:build windows
//________________________________________________________
// Algorithm 1 (improved + optimized + external HTML).
// Description: Go desktop application with resizable borderless rounded window via webview2
//________________________________________________________
package main

//________________________________________________________
// Imports
import (
	"container/heap" // Heap implementation
	"encoding/json" // JSON encoding
	"fmt" // Formatting
	"math" // Math functions
	"net/http" // HTTP server
	"os" // OS functions
	"os/exec" // Command execution
	"path/filepath" // Path manipulation
	"runtime" // Runtime info
	"strconv" // String conversion
	"strings" // String manipulation
	"sync" // Synchronization
	"syscall" // Syscalls for Windows
	"time" // Time functions
	"unsafe" // Unsafe pointers

	webview "github.com/jchv/go-webview2" // Webview library
)

//________________________________________________________
// Constants and globals
const serverPort = ":8123" // Server port
var indexHTML string // Global variable for HTML (loaded from file on startup)

//________________________________________________________
// Structs definition
type OrderItem struct {
	PageNum  int // Page number
	Quantity int // Order quantity
}

type CalcRequest struct {
	OvershootPct float64 `json:"overshoot"` // Max overshoot percentage
	Capacity     int     `json:"capacity"` // Sheet capacity
	Orders       string  `json:"orders"` // Orders string
}

type ItemReport struct {
	PageNum   int     `json:"page_num"` // Page number
	Target    int     `json:"target"` // Target quantity
	Produced  int     `json:"produced"` // Produced quantity
	Overshoot float64 `json:"overshoot"` // Overshoot percentage
	SlotsStr  string  `json:"slots_str"` // Slots string representation
	SlotsList []int   `json:"slots_list"` // Slots list
}

type CalcResponse struct {
	Success        bool         `json:"success"` // Success flag
	Message        string       `json:"message"` // Response message
	TotalSheets    int          `json:"total_sheets"` // Total sheets
	TotalForms     int          `json:"total_forms"` // Total forms
	Forms          []int        `json:"forms"` // Forms list
	ItemReports    []ItemReport `json:"item_reports"` // Item reports
	TotalOrdered   int          `json:"total_ordered"` // Total ordered
	TotalProduced  int          `json:"total_produced"` // Total produced
	TotalOvershoot float64      `json:"total_overshoot"` // Total overshoot percentage
	PrintCodes     []string     `json:"print_codes"` // Print codes list
	FormNames      []string     `json:"form_names"` // Form names list
}

type FormBlock struct {
	FormName     string `json:"form_name"` // Form name
	FormNameHtml string `json:"form_name_html"` // Form name HTML
	CodeLine     string `json:"code_line"` // Code line
}

type ExtendedCalcResponse struct {
	CalcResponse // Embedded response
	FormBlocks []FormBlock `json:"form_blocks"` // Form blocks
	Logs       string      `json:"logs"` // Calculation logs
}

type FindMinOvershootRequest struct {
	Capacity         int     `json:"capacity"` // Sheet capacity
	Orders           string  `json:"orders"` // Orders string
	CurrentOvershoot float64 `json:"current_overshoot"` // Current overshoot
	StartForms       int     `json:"start_forms"` // Starting forms count
}

type FindMinOvershootResponse struct {
	Success   bool    `json:"success"` // Success flag
	Overshoot float64 `json:"overshoot"` // Optimal overshoot
	Message   string  `json:"message"` // Response message
}

//________________________________________________________
// Priority queue for D'Hondt allocation
type dhondtItem struct {
	index int // Item index
	score float64 // Current D'Hondt score
	slots int // Allocated slots
	need  int // Quantity needed (used for tie-breaking optimally)
}
type dhondtPQ []*dhondtItem

//________________________________________________________
// Priority queue length
func (pq dhondtPQ) Len() int           { return len(pq) }

//________________________________________________________
// Priority queue less comparator (Max-Heap with smart tie-breaking)
func (pq dhondtPQ) Less(i, j int) bool {
	diff := pq[i].score - pq[j].score // Compare scores
	if math.Abs(diff) > 1e-9 {
		return diff > 0 // Max-Heap based on score
	}
	slotsI := pq[i].slots // Get slots for i
	if slotsI == 0 { slotsI = 1 } // Prevent division by zero
	currI := float64(pq[i].need) / float64(slotsI) // Current ratio i
	
	slotsJ := pq[j].slots // Get slots for j
	if slotsJ == 0 { slotsJ = 1 } // Prevent division by zero
	currJ := float64(pq[j].need) / float64(slotsJ) // Current ratio j
	
	diffCurr := currI - currJ // Compare current ratios
	if math.Abs(diffCurr) > 1e-9 {
		return diffCurr > 0 // Tie-breaker 1: highest current need/slots ensures R stays low
	}
	return pq[i].need > pq[j].need // Tie-breaker 2: larger overall need
}

//________________________________________________________
// Priority queue swap
func (pq dhondtPQ) Swap(i, j int)      { pq[i], pq[j] = pq[j], pq[i] }

//________________________________________________________
// Priority queue push
func (pq *dhondtPQ) Push(x any)        { *pq = append(*pq, x.(*dhondtItem)) }

//________________________________________________________
// Priority queue pop
func (pq *dhondtPQ) Pop() any {
	old := *pq // Get old queue
	n := len(old) // Get length
	item := old[n-1] // Get last item
	*pq = old[0 : n-1] // Remove last item
	return item // Return item
}

//________________________________________________________
// Global cache
var calcCache = struct {
	sync.RWMutex
	m map[string]ExtendedCalcResponse
}{m: make(map[string]ExtendedCalcResponse)}

//________________________________________________________
// Helper to decode JSON from request
func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close() // Close body when done
	return json.NewDecoder(r.Body).Decode(v) // Decode JSON
}

//________________________________________________________
// Set icon from PNG file
func setIconFromPNGFile(hwnd uintptr, pngPath string) {
	if runtime.GOOS != "windows" {
		return // Skip if not windows
	}
	pngData, err := os.ReadFile(pngPath) // Read PNG file
	if err != nil {
		return // Exit on error
	}
	size := len(pngData) // Get file size
	header := []byte{0, 0, 1, 0, 1, 0, 0, 0, 0, 0, 1, 0, 32, 0, byte(size), byte(size >> 8), byte(size >> 16), byte(size >> 24), 22, 0, 0, 0} // Create ICO header
	icoPath := filepath.Join(os.TempDir(), fmt.Sprintf("temp_logo_%d.ico", time.Now().UnixNano())) // Generate temp ICO path
	if err := os.WriteFile(icoPath, append(header, pngData...), 0644); err != nil {
		return // Exit on write error
	}
	defer os.Remove(icoPath) // Remove temp file when done

	user32 := syscall.NewLazyDLL("user32.dll") // Load user32.dll
	loadImage := user32.NewProc("LoadImageW") // Load LoadImageW proc
	sendMessage := user32.NewProc("SendMessageW") // Load SendMessageW proc
	pathPtr, _ := syscall.UTF16PtrFromString(icoPath) // Convert path to UTF16
	hIcon, _, _ := loadImage.Call(0, uintptr(unsafe.Pointer(pathPtr)), 1, 0, 0, 0x00000010) // Call LoadImageW
	if hIcon == 0 {
		return // Exit if icon not loaded
	}
	const WM_SETICON = 0x0080 // WM_SETICON constant
	sendMessage.Call(hwnd, WM_SETICON, 0, hIcon) // Set small icon
	sendMessage.Call(hwnd, WM_SETICON, 1, hIcon) // Set large icon
}

//________________________________________________________
// Setup frameless window
func setupFramelessWindow(w webview.WebView) {
	if runtime.GOOS != "windows" {
		return // Skip if not windows
	}
	hwnd := uintptr(w.Window()) // Get window handle
	user32 := syscall.NewLazyDLL("user32.dll") // Load user32.dll
	setWindowLongPtr := user32.NewProc("SetWindowLongPtrW") // Load SetWindowLongPtrW proc
	if setWindowLongPtr.Find() != nil {
		setWindowLongPtr = user32.NewProc("SetWindowLongA") // Fallback to SetWindowLongA
	}
	getWindowLongPtr := user32.NewProc("GetWindowLongPtrW") // Load GetWindowLongPtrW proc
	if getWindowLongPtr.Find() != nil {
		getWindowLongPtr = user32.NewProc("GetWindowLongA") // Fallback to GetWindowLongA
	}
	const GWL_STYLE = 0xFFFFFFF0 // GWL_STYLE constant
	const WS_POPUP = 0x80000000 // WS_POPUP constant
	const WS_THICKFRAME = 0x00040000 // WS_THICKFRAME constant
	const WS_SYSMENU = 0x00080000 // WS_SYSMENU constant
	const WS_MAXIMIZEBOX = 0x00010000 // WS_MAXIMIZEBOX constant
	const WS_MINIMIZEBOX = 0x00020000 // WS_MINIMIZEBOX constant

	style, _, _ := getWindowLongPtr.Call(hwnd, uintptr(GWL_STYLE)) // Get current style
	newStyle := (style &^ 0x00C00000) | WS_POPUP | WS_THICKFRAME | WS_SYSMENU | WS_MAXIMIZEBOX | WS_MINIMIZEBOX // Calculate new style
	setWindowLongPtr.Call(hwnd, uintptr(GWL_STYLE), newStyle) // Set new style
	updateRoundedRegion(w) // Update rounded corners
}

//________________________________________________________
// Update rounded window region
func updateRoundedRegion(w webview.WebView) {
	if runtime.GOOS != "windows" {
		return // Skip if not windows
	}
	hwnd := uintptr(w.Window()) // Get window handle
	user32 := syscall.NewLazyDLL("user32.dll") // Load user32.dll
	gdi32 := syscall.NewLazyDLL("gdi32.dll") // Load gdi32.dll
	createRoundRectRgn := gdi32.NewProc("CreateRoundRectRgn") // Load CreateRoundRectRgn proc
	setWindowRgn := user32.NewProc("SetWindowRgn") // Load SetWindowRgn proc
	getWindowRect := user32.NewProc("GetWindowRect") // Load GetWindowRect proc
	type rect struct{ Left, Top, Right, Bottom int32 } // Rect struct
	var rObj rect // Rect object
	getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rObj))) // Get window rect
	width := int(rObj.Right - rObj.Left) // Calculate width
	height := int(rObj.Bottom - rObj.Top) // Calculate height
	hrgn, _, _ := createRoundRectRgn.Call(0, 0, uintptr(width), uintptr(height), 16, 16) // Create region
	if hrgn != 0 {
		setWindowRgn.Call(hwnd, hrgn, 1) // Set window region
	}
}

//________________________________________________________
// Drag window handler
func dragWindow(w webview.WebView) {
	if runtime.GOOS != "windows" {
		return // Skip if not windows
	}
	hwnd := uintptr(w.Window()) // Get window handle
	user32 := syscall.NewLazyDLL("user32.dll") // Load user32.dll
	releaseCapture := user32.NewProc("ReleaseCapture") // Load ReleaseCapture proc
	sendMessage := user32.NewProc("SendMessageW") // Load SendMessageW proc
	const WM_NCLBUTTONDOWN = 0x00A1 // WM_NCLBUTTONDOWN constant
	const HTCAPTION = 2 // HTCAPTION constant
	releaseCapture.Call() // Release capture
	sendMessage.Call(hwnd, WM_NCLBUTTONDOWN, uintptr(HTCAPTION), 0) // Send message to drag
}

//________________________________________________________
// Main application entry point
func main() {
	execDir, err := os.Executable() // Get executable path
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot determine executable path:", err) // Print error
		os.Exit(1) // Exit on error
	}
	baseDir := filepath.Dir(execDir) // Get base directory

	htmlPath := filepath.Join(baseDir, "settings", "index.html") // Load HTML from settings folder
	htmlData, err := os.ReadFile(htmlPath) // Read HTML file
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read index.html from %s: %v\n", htmlPath, err) // Print error
		fmt.Fprintln(os.Stderr, "Please ensure settings/index.html exists next to the executable.") // Print info
		os.Exit(1) // Exit on error
	}
	indexHTML = string(htmlData) // Store HTML to global var

	splashPath := filepath.Join(baseDir, "settings", "splash.exe") // Run splash screen if exists
	if _, err := os.Stat(splashPath); err == nil {
		cmd := exec.Command(splashPath, "500") // Create splash command
		cmd.Dir = filepath.Join(baseDir, "settings") // Set working directory
		_ = cmd.Start() // Start splash screen
	}

	go func() { // Start HTTP server
		http.HandleFunc("/", serveUI) // Map UI handler
		http.HandleFunc("/api/calculate", handleCalc) // Map calculation handler
		http.HandleFunc("/api/find_min_overshoot", handleFindMinOvershoot) // Map min overshoot handler
		http.HandleFunc("/api/close", handleClose) // Map close handler
		_ = http.ListenAndServe(serverPort, nil) // Start listening
	}()

	time.Sleep(200 * time.Millisecond) // Wait for server to start
	w := webview.New(false) // Create new webview
	defer w.Destroy() // Destroy on exit
	w.SetTitle("Layout-sheets-calc") // Set title
	setupFramelessWindow(w) // Setup frameless window

	if runtime.GOOS == "windows" { // Set window size: Y=0, height 95% of screen, X is preserved
		user32 := syscall.NewLazyDLL("user32.dll") // Load user32.dll
		getSystemMetrics := user32.NewProc("GetSystemMetrics") // Load GetSystemMetrics proc
		screenHeight, _, _ := getSystemMetrics.Call(1) // Get screen height
		if screenHeight > 0 {
			newHeight := int(float64(screenHeight) * 0.95) // Calculate new height
			getWindowRect := user32.NewProc("GetWindowRect") // Load GetWindowRect proc
			var rect struct{ Left, Top, Right, Bottom int32 } // Rect struct
			hwnd := uintptr(w.Window()) // Get window handle
			getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect))) // Get current rect
			currentX := rect.Left // Preserve X
			currentWidth := rect.Right - rect.Left // Preserve width
			setWindowPos := user32.NewProc("SetWindowPos") // Load SetWindowPos proc
			const SWP_NOZORDER = 0x0004 // SWP_NOZORDER constant
			setWindowPos.Call(hwnd, 0, uintptr(currentX), uintptr(0), uintptr(currentWidth), uintptr(newHeight), uintptr(SWP_NOZORDER)) // Set new position and size
			updateRoundedRegion(w) // Update rounded region again
		}
	}

	logoPath := filepath.Join(baseDir, "settings", "logo.png") // Set icon path
	setIconFromPNGFile(uintptr(w.Window()), logoPath) // Set window icon

	w.Bind("startDrag", func() { dragWindow(w) }) // Bind drag function
	w.Bind("updateWindowRegion", func() { updateRoundedRegion(w) }) // Bind update region function
	w.Bind("closeAppNative", func() { // Bind close function
		w.Destroy() // Destroy window
		os.Exit(0) // Exit app
	})
	w.Navigate("http://localhost" + serverPort) // Navigate to local server
	w.Run() // Run webview loop
}

//________________________________________________________
// Serve UI HTML
func serveUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8") // Set content type
	w.Write([]byte(indexHTML)) // Write HTML response
}

//________________________________________________________
// Handle close request
func handleClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed) // Return error if not POST
		return // Exit handler
	}
	w.WriteHeader(http.StatusOK) // Set OK status
	w.Write([]byte(`{"success":true}`)) // Write success response
	go func() { // Async exit
		time.Sleep(100 * time.Millisecond) // Short delay
		os.Exit(0) // Exit app
	}()
}

//________________________________________________________
// Calculate layout heuristic
func calculateHeuristic(items []OrderItem, capacity int, maxOvr float64) ([][]int, []int, []string) {
	var logs []string // Log array
	M := len(items) // Item count
	produced := make([]int, M) // Produced quantities
	var forms [][]int // Layout forms
	var runs []int // Run counts

	maxAllowed := make([]int, M) // Max allowed quantities
	for i, it := range items {
		maxAllowed[i] = int(math.Floor(float64(it.Quantity) * (1.0 + maxOvr/100.0))) // Calc max allowed
	}

	remaining := make([]int, 0, M) // Remaining indices
	iteration := 1 // Iteration counter

	for {
		remaining = remaining[:0] // Clear remaining
		for i := 0; i < M; i++ {
			if produced[i] < items[i].Quantity {
				remaining = append(remaining, i) // Add to remaining if not fulfilled
			}
		}
		if len(remaining) == 0 {
			logs = append(logs, fmt.Sprintf("\nDONE: All %d items placed.", M)) // Log completion
			break // Exit loop
		}
		if iteration > 1000 {
			logs = append(logs, "\nSAFETY STOP: 1000 cycles.") // Log safety stop
			break // Exit loop
		}

		logs = append(logs, fmt.Sprintf("\n--- CYCLE %d ---", iteration)) // Log cycle
		logs = append(logs, fmt.Sprintf("Items left: %d", len(remaining))) // Log items left

		slots := make([]int, M) // Slots array
		unallocated := capacity // Unallocated slots
		pq := make(dhondtPQ, 0, len(remaining)) // Priority queue

		if unallocated >= len(remaining) {
			for _, i := range remaining {
				slots[i] = 1 // Pre-allocate 1 slot
				unallocated-- // Decrement unallocated
			}
			logs = append(logs, "Pre-allocated 1 slot per item.") // Log pre-allocation
		} else {
			logs = append(logs, "Warning: capacity < items left.") // Log warning
		}

		for _, i := range remaining {
			need := items[i].Quantity - produced[i] // Calc need
			maxRemainingOvr := maxAllowed[i] - produced[i] // Calc max remaining overshoot
			if slots[i]+1 <= maxRemainingOvr {
				score := float64(need) / float64(slots[i]+1) // Calc score
				pq = append(pq, &dhondtItem{index: i, score: score, slots: slots[i], need: need}) // Add to PQ
			}
		}
		heap.Init(&pq) // Init heap

		for unallocated > 0 && pq.Len() > 0 {
			top := heap.Pop(&pq).(*dhondtItem) // Pop top item
			top.slots++ // Increment item slots
			slots[top.index]++ // Increment global slots
			unallocated-- // Decrement unallocated

			maxRemainingOvr := maxAllowed[top.index] - produced[top.index] // Recalc max remaining
			if top.slots+1 <= maxRemainingOvr {
				need := items[top.index].Quantity - produced[top.index] // Recalc need
				top.score = float64(need) / float64(top.slots+1) // Recalc score
				// need stays the same for priority queue tiebreaker logic over current sheet
				heap.Push(&pq, top) // Push back to PQ
			}
		}
		if unallocated > 0 {
			logs = append(logs, "Stopped allocation: overshoot constraints.") // Log stop reason
		}

		var allocLog strings.Builder // Allocation log builder
		for i, s := range slots {
			if s > 0 {
				need := items[i].Quantity - produced[i] // Calc need
				allocLog.WriteString(fmt.Sprintf("[Pg %d : need %d → %d slots] ", items[i].PageNum, need, s)) // Append log
			}
		}
		logs = append(logs, fmt.Sprintf("Slot distribution (%d places):", capacity)) // Log distribution
		logs = append(logs, "  "+allocLog.String()) // Log details

		rLowerBound, rUpperBound := 0, math.MaxInt32 // Bounds for runs
		for i, s := range slots {
			if s > 0 {
				need := items[i].Quantity - produced[i] // Calc need
				reqMin := (need + s - 1) / s // Calc min required runs
				if reqMin > rLowerBound {
					rLowerBound = reqMin // Update lower bound
				}
				maxRemainingOvr := maxAllowed[i] - produced[i] // Calc max remaining
				maxAllowedR := maxRemainingOvr / s // Calc max allowed runs
				if maxAllowedR < rUpperBound {
					rUpperBound = maxAllowedR // Update upper bound
				}
			}
		}
		R := 1 // Run length
		if rLowerBound <= rUpperBound {
			R = rLowerBound // Set R to lower bound
			logs = append(logs, fmt.Sprintf("Can complete all, R = %d", R)) // Log completion
		} else {
			R = rUpperBound // Set R to upper bound
			if R <= 0 {
				R = 1 // Ensure R is at least 1
			}
			logs = append(logs, fmt.Sprintf("Bottleneck limits R to %d", R)) // Log bottleneck
		}

		for i, s := range slots {
			if s > 0 {
				produced[i] += R * s // Update produced quantity
			}
		}
		forms = append(forms, slots) // Add slots to forms
		runs = append(runs, R) // Add R to runs
		iteration++ // Increment iteration
	}

	layouts := make([][]int, M) // Layouts matrix
	for i := 0; i < M; i++ {
		layouts[i] = make([]int, len(forms)) // Init row
		for j := 0; j < len(forms); j++ {
			layouts[i][j] = forms[j][i] // Transpose forms to layouts
		}
	}
	return layouts, runs, logs // Return results
}

//________________________________________________________
// Core calculation logic
func calculateCore(req CalcRequest) (ExtendedCalcResponse, error) {
	parts := strings.Fields(req.Orders) // Split orders
	var items []OrderItem // Items list
	var orders []int // Quantities list
	for _, p := range parts {
		sub := strings.Split(p, "*") // Split by asterisk
		if len(sub) == 2 {
			pageNum, e1 := strconv.Atoi(sub[0]) // Parse page num
			qty, e2 := strconv.Atoi(sub[1]) // Parse quantity
			if e1 == nil && e2 == nil && qty > 0 {
				items = append(items, OrderItem{PageNum: pageNum, Quantity: qty}) // Add to items
				orders = append(orders, qty) // Add to quantities
			}
		}
	}
	if len(orders) == 0 {
		return ExtendedCalcResponse{}, fmt.Errorf("No valid orders") // Error on empty
	}
	layouts, runs, logs := calculateHeuristic(items, req.Capacity, req.OvershootPct) // Run heuristic
	if len(runs) == 0 {
		return ExtendedCalcResponse{}, fmt.Errorf("No layout found") // Error if no layout
	}
	resp := buildResponse(runs, layouts, items, req.Capacity) // Build response
	resp.TotalOvershoot = ((float64(resp.TotalProduced) - float64(resp.TotalOrdered)) / float64(resp.TotalOrdered)) * 100.0 // Calculate total overshoot
	resp.Logs = strings.Join(logs, "\n") // Join logs
	return resp, nil // Return response
}

//________________________________________________________
// Build calculation response
func buildResponse(R []int, layouts [][]int, items []OrderItem, capacity int) ExtendedCalcResponse {
	totalSheets := 0 // Total sheets counter
	for _, r := range R {
		totalSheets += r // Sum runs
	}
	var itemReports []ItemReport // Item reports list
	totalOrdered, totalProduced := 0, 0 // Order and production counters
	var formBlocks []FormBlock // Form blocks list

	for j := 0; j < len(R); j++ {
		fName := fmt.Sprintf("Sheet %d %d", j+1, R[j]) // Format sheet name
		fNameHtml := fmt.Sprintf("Sheet %d <span class=\"sheet-badge\">%d</span> Pcs", j+1, R[j]) // Format HTML name
		var b strings.Builder // String builder for line
		for i := 0; i < len(items); i++ {
			if layouts[i][j] > 0 {
				b.WriteString(fmt.Sprintf("%d*%d ", items[i].PageNum, layouts[i][j])) // Append item to line
			}
		}
		line := strings.TrimSpace(b.String()) // Trim line
		formBlocks = append(formBlocks, FormBlock{
			FormName:     fName, // Set name
			FormNameHtml: fNameHtml, // Set HTML name
			CodeLine:     line, // Set code line
		})
	}

	for i, item := range items {
		produced := 0 // Produced counter
		var slotsList []int // Slots list
		for j := 0; j < len(R); j++ {
			slots := layouts[i][j] // Get slots
			produced += slots * R[j] // Add to produced
			slotsList = append(slotsList, slots) // Add to list
		}
		totalOrdered += item.Quantity // Add to total ordered
		totalProduced += produced // Add to total produced
		overshootPct := ((float64(produced) - float64(item.Quantity)) / float64(item.Quantity)) * 100.0 // Calc item overshoot
		itemReports = append(itemReports, ItemReport{
			PageNum:   item.PageNum, // Set page num
			Target:    item.Quantity, // Set target quantity
			Produced:  produced, // Set produced quantity
			Overshoot: overshootPct, // Set overshoot
			SlotsStr:  strings.Trim(strings.Join(strings.Fields(fmt.Sprint(slotsList)), " | "), "[]"), // Set slots string
			SlotsList: slotsList, // Set slots list
		})
	}
	globalOvershoot := ((float64(totalProduced) - float64(totalOrdered)) / float64(totalOrdered)) * 100.0 // Calc global overshoot
	base := CalcResponse{
		Success:        true, // Set success
		TotalSheets:    totalSheets, // Set total sheets
		TotalForms:     len(R), // Set total forms
		Forms:          R, // Set forms
		ItemReports:    itemReports, // Set item reports
		TotalOrdered:   totalOrdered, // Set total ordered
		TotalProduced:  totalProduced, // Set total produced
		TotalOvershoot: globalOvershoot, // Set global overshoot
	}
	return ExtendedCalcResponse{
		CalcResponse: base, // Set base response
		FormBlocks:   formBlocks, // Set form blocks
	}
}

//________________________________________________________
// Handle calculation request
func handleCalc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed) // Method error
		return // Exit handler
	}
	var req CalcRequest // Request struct
	if err := decodeJSON(r, &req); err != nil {
		sendJSON(w, CalcResponse{Success: false, Message: "Invalid JSON"}) // JSON error
		return // Exit handler
	}
	cacheKey := fmt.Sprintf("%f|%d|%s", req.OvershootPct, req.Capacity, req.Orders) // Create cache key
	calcCache.RLock() // Lock cache
	cached, found := calcCache.m[cacheKey] // Check cache
	calcCache.RUnlock() // Unlock cache
	if found {
		sendJSON(w, cached) // Send cached result
		return // Exit handler
	}
	resp, err := calculateCore(req) // Calculate core
	if err != nil {
		sendJSON(w, ExtendedCalcResponse{
			CalcResponse: CalcResponse{Success: false, Message: err.Error()}, // Error response
		})
		return // Exit handler
	}
	calcCache.Lock() // Lock cache for write
	calcCache.m[cacheKey] = resp // Save to cache
	calcCache.Unlock() // Unlock cache
	sendJSON(w, resp) // Send response
}

//________________________________________________________
// Handle min overshoot search request
func handleFindMinOvershoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed) // Method error
		return // Exit handler
	}
	var req FindMinOvershootRequest // Request struct
	if err := decodeJSON(r, &req); err != nil {
		sendJSON(w, FindMinOvershootResponse{Success: false, Message: "Invalid JSON"}) // JSON error
		return // Exit handler
	}
	parts := strings.Fields(req.Orders) // Split orders
	minSheets := int(math.Ceil(float64(len(parts)) / float64(req.Capacity))) // Calc min sheets
	if req.StartForms <= minSheets {
		sendJSON(w, FindMinOvershootResponse{Success: false, Message: "Already at minimum"}) // Already min
		return // Exit handler
	}
	low, high := int(req.CurrentOvershoot)+1, 10000 // Init bounds
	best := -1 // Init best
	for low <= high {
		mid := (low + high) / 2 // Calc mid
		calcReq := CalcRequest{
			OvershootPct: float64(mid), // Set mid overshoot
			Capacity:     req.Capacity, // Set capacity
			Orders:       req.Orders, // Set orders
		}
		resp, err := calculateCore(calcReq) // Calculate core
		if err != nil {
			sendJSON(w, FindMinOvershootResponse{Success: false, Message: err.Error()}) // Error response
			return // Exit handler
		}
		if resp.TotalForms < req.StartForms {
			best = mid // Update best
			high = mid - 1 // Search lower
		} else {
			low = mid + 1 // Search higher
		}
	}
	if best == -1 {
		sendJSON(w, FindMinOvershootResponse{Success: false, Message: "Cannot reduce further"}) // Cannot reduce
		return // Exit handler
	}
	sendJSON(w, FindMinOvershootResponse{Success: true, Overshoot: float64(best)}) // Send best
}

//________________________________________________________
// Helper to send JSON response
func sendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json") // Set content type
	json.NewEncoder(w).Encode(data) // Encode and send JSON
}