//________________________________________________________
// Description: Go desktop application with resizable borderless rounded window via webview2
//________________________________________________________
package main

//________________________________________________________
import (
    "bytes"
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
    "syscall"
    "time"
    "unsafe"

    webview "github.com/jchv/go-webview2"
)

//________________________________________________________
const serverPort = ":8080"

//________________________________________________________
type State int32

//________________________________________________________
type CalcRequest struct {
    OvershootPct float64 `json:"overshoot"`
    Capacity     int     `json:"capacity"`
    Orders       string  `json:"orders"`
}

//________________________________________________________
type ItemReport struct {
    ID         int     `json:"id"`
    Target     int     `json:"target"`
    Produced   int     `json:"produced"`
    Overshoot  float64 `json:"overshoot"`
    SlotsStr   string  `json:"slots_str"`
    SlotsList  []int   `json:"slots_list"`
}

//________________________________________________________
type CalcResponse struct {
    Success            bool         `json:"success"`
    Message            string       `json:"message"`
    TotalSheets        int          `json:"total_sheets"`
    TotalForms         int          `json:"total_forms"`
    Forms              []int        `json:"forms"`
    ItemReports        []ItemReport `json:"item_reports"`
    TotalOrdered       int          `json:"total_ordered"`
    TotalProduced      int          `json:"total_produced"`
    TotalOvershoot     float64      `json:"total_overshoot"`
    PrintCodes         []string     `json:"print_codes"`
    FormNames          []string     `json:"form_names"`
    UpdatedOvershoot   float64      `json:"updated_overshoot"`
}

//________________________________________________________
const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Layout-sheets-calc.Magicon.top by Levchuk</title>
    <style>
        html, body { height: 100%; width: 100%; background-color: #f0f2f5; margin: 0; padding: 0; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; color: #333; font-size: 16.5px; overflow: hidden; -webkit-user-select: none; user-select: none; box-sizing: border-box; }
        .titlebar { background: #EFF9DE; color: #432818; height: 32px; display: flex; align-items: center; justify-content: space-between; padding: 0 0 0 10px; box-sizing: border-box; cursor: default; flex-shrink: 0; }
        .titlebar-title { font-size: 13px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; pointer-events: none; }
        .titlebar-controls { display: flex; align-items: center; gap: 0; height: 100%; padding-right: 4px; }

        .container { width: 100%; height: calc(100% - 32px); box-sizing: border-box; position: relative; padding: 8px; display: flex; flex-direction: column; overflow-y: auto; }

        .header-panel { background: white; padding: 10px; border-radius: 6px; box-shadow: 0 1px 4px rgba(0,0,0,0.05); width: 100%; box-sizing: border-box; position: relative; flex-shrink: 0; }
        .top-row { display: flex; gap: 8px; align-items: flex-end; margin-bottom: 8px; padding-right: 50px; }
        .input-group { display: flex; flex-direction: column; gap: 3px; flex-shrink: 0; }
        .input-group.full-width { width: 100%; }
        .input-group label { font-size: 13.5px; font-weight: bold; color: #555; }
        .input-group input, .input-group textarea { padding: 4px 8px; border: 1px solid #ccc; border-radius: 4px; font-size: 16.5px; font-weight: bold; outline: none; transition: border 0.2s; box-sizing: border-box; font-family: inherit; -webkit-user-select: text; user-select: text; }
        .input-group input:focus, .input-group textarea:focus { border-color: #007bff; }

        #overshoot { width: 75px; height: 33px; }
        #capacity { width: 85px; height: 33px; }
        #orders { width: 100%; min-height: 33px; resize: none; overflow-y: hidden; line-height: 1.3; box-sizing: border-box; }

        .action-buttons { position: absolute; right: 10px; top: 10px; display: flex; gap: 4px; z-index: 100; }
        button.icon-btn { padding: 0; border: none; border-radius: 4px; cursor: pointer; transition: background 0.2s; width: 36px; height: 33px; display: flex; align-items: center; justify-content: center; }
        button.icon-btn svg { width: 18px; height: 18px; fill: white; }

        button.titlebar-btn { padding: 0; border: none; background: transparent; cursor: pointer; width: 32px; height: 24px; border-radius: 4px; display: flex; align-items: center; justify-content: center; transition: background 0.2s; }
        button.titlebar-btn.close { background: transparent; border-radius: 4px; width: 28px; height: 24px; transition: background 0.2s, fill 0.2s; }
        button.titlebar-btn.close:hover { background: #dc3545; }
        button.titlebar-btn svg { width: 14px; height: 14px; fill: #333333; stroke: #333333; stroke-width: 0; display: block; margin: auto; transition: fill 0.2s; }
        button.titlebar-btn.close svg { fill: #333333; stroke: #333333; }
        button.titlebar-btn.close:hover svg { fill: white; stroke: white; }

        button#calcBtn { background: #007bff; }
        button#calcBtn:hover { background: #0056b3; }
        button#calcBtn:disabled { background: #999; cursor: not-allowed; }

        .report-panel { margin-top: 8px; background: white; padding: 10px; border-radius: 6px; box-shadow: 0 1px 4px rgba(0,0,0,0.05); display: none; width: 100%; box-sizing: border-box; }

        .summary-cards { display: flex; gap: 6px; margin-bottom: 10px; flex-wrap: nowrap; width: 100%; box-sizing: border-box; }
        .card { background: #f8f9fa; padding: 6px 8px; border-radius: 4px; border-left: 3px solid #007bff; flex: 1; min-width: 0; box-sizing: border-box; }
        .card.warning { border-left-color: #ffc107; }
        .card.success { border-left-color: #2d2d2d; }
        .card-title { font-size: 10px; color: #666; text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 1px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .card-value { font-size: 16px; font-weight: bold; color: #222; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

        .section-title { font-size: 16px; font-weight: bold; margin: 8px 0 3px 0; color: #333; }

        .code-block-container { margin-bottom: 8px; width: 100%; box-sizing: border-box; display: flex; align-items: stretch; gap: 6px; }
        .print-codes { color: #ffffff; background: #2d2d2d; padding: 8px 12px; border-radius: 4px; font-family: monospace; white-space: normal; word-break: break-all; overflow-wrap: break-word; font-size: 16.5px; flex-grow: 1; box-sizing: border-box; border: 1px solid #2d2d2d; display: flex; align-items: center; -webkit-user-select: text; user-select: text; }
        .copy-btn { padding: 0 14px; background: #6c757d; color: white; border: none; border-radius: 4px; font-size: 15px; cursor: pointer; height: auto; display: inline-flex; align-items: center; justify-content: center; flex-shrink: 0; font-weight: bold; }
        .copy-btn:hover { background: #5a6268; }

        table { width: 100%; border-collapse: collapse; margin-bottom: 6px; -webkit-user-select: text; user-select: text; table-layout: fixed; }
        th.col-item, td.col-item { width: 70px; }
        th.col-overage, td.col-overage { width: 80px; text-align: right; }
        th.col-order, td.col-order { width: 100px; text-align: right; }
        th.col-quantity, td.col-quantity { width: auto; }

        th, td { padding: 4px 8px; text-align: left; border-bottom: 1px solid #eee; font-size: 16.5px; vertical-align: middle; }
        th { background-color: #f8f9fa; font-weight: 600; color: #444; padding-top: 4px; padding-bottom: 4px; }
        tr:hover { background-color: #f1f4f8; }
        .overshoot { font-weight: bold; }
        .overshoot.good { color: #28a745; }

        .quantity-chips { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; }
        .quantity-chip { background-color: #2d2d2d; color: #ffffff; padding: 2px 4px; border-radius: 4px; min-width: 36px; text-align: center; font-size: 15px; font-weight: 600; display: inline-block; box-sizing: border-box; }

        .error { color: #dc3545; font-weight: bold; padding: 10px; background: #ffe6e6; border-radius: 4px; display: none; margin-top: 8px; width: 100%; box-sizing: border-box; font-size: 15px; }
    </style>
</head>
<body>
    <div class="titlebar" id="titleBar">
        <div class="titlebar-title">Layout-sheets-calc.MagicON.Top by Levchuk V.N. </div>
        <div class="titlebar-controls">
            <button class="titlebar-btn close" onclick="closeApp()" title="Close Application">
                <svg viewBox="0 0 24 24"><path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>
            </button>
        </div>
    </div>

    <div class="container">
        <div class="action-buttons">
            <button id="calcBtn" class="icon-btn" onclick="calculate()" title="Calculate">
                <svg viewBox="0 0 24 24"><path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z"/></svg>
            </button>
        </div>

        <div class="header-panel">
            <div class="top-row">
                <div class="input-group">
                    <label>Overshoot (%)</label>
                    <input type="number" id="overshoot" value="25" min="0" max="100">
                </div>
                <div class="input-group">
                    <label>Positions per page</label>
                    <input type="number" id="capacity" value="322" min="1">
                </div>
            </div>
            <div class="input-group full-width">
                <label>Orders (space separated)</label>
                <textarea id="orders" rows="1" oninput="autoResize(this)">500 600 400 500 500 500 800 400 600 400 500 800 400 800 500 500 400 500 600 800 600</textarea>
            </div>
        </div>

        <div id="errorBox" class="error"></div>

        <div id="reportPanel" class="report-panel">
            <div class="summary-cards">
                <div class="card" style="display: none;">
                    <div class="card-title">Sheets for Print</div>
                    <div class="card-value" id="valSheets">-</div>
                </div>
                <div class="card">
                    <div class="card-title">Unique Sheets</div>
                    <div class="card-value" id="valForms">-</div>
                </div>
                <div class="card warning">
                    <div class="card-title">Total Overage</div>
                    <div class="card-value" id="valTotalOvershoot">-</div>
                </div>
                <div class="card success" style="border-left-color: #2d2d2d; background: #2d2d2d;">
                    <div class="card-title" style="color: #cccccc;">Ordered / Produced</div>
                    <div class="card-value" style="font-size: 15px; margin-top: 2px; color: #ffffff;"><span id="valOrdered"></span> / <span id="valProduced"></span></div>
                </div>
            </div>

            <div id="resultsContainer"></div>
        </div>
    </div>

    <script>
        // Auto-resizes textarea based on content
        function autoResize(textarea) {
            textarea.style.height = 'auto';
            textarea.style.height = textarea.scrollHeight + 'px';
        }

        window.addEventListener('DOMContentLoaded', () => {
            const ta = document.getElementById('orders');
            autoResize(ta);
            calculate();

            const titleBar = document.getElementById('titleBar');

            titleBar.addEventListener('mousedown', (e) => {
                if (e.target.closest('.titlebar-controls')) return;
                
                if (window.startDrag) {
                    window.startDrag();
                    return;
                }
            });
        });

        window.addEventListener('resize', () => {
            const ta = document.getElementById('orders');
            autoResize(ta);
            
            if (window.updateWindowRegion) {
                window.updateWindowRegion();
            }
        });

        // Sends request to terminate application
        async function closeApp() {
            if (window.closeAppNative) {
                window.closeAppNative();
            } else {
                try {
                    await fetch('/api/close', { method: 'POST' });
                } catch (e) {}
                window.close();
            }
        }

        // Handles layout calculation execution
        async function calculate() {
            const btn = document.getElementById('calcBtn');
            const errorBox = document.getElementById('errorBox');
            const reportPanel = document.getElementById('reportPanel');

            btn.disabled = true;
            errorBox.style.display = 'none';
            reportPanel.style.display = 'none';

            const payload = {
                overshoot: parseFloat(document.getElementById('overshoot').value),
                capacity: parseInt(document.getElementById('capacity').value),
                orders: document.getElementById('orders').value
            };

            try {
                const response = await fetch('/api/calculate', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });

                const data = await response.json();

                if (!data.success) {
                    errorBox.textContent = data.message;
                    errorBox.style.display = 'block';
                } else {
                    if (data.updated_overshoot !== undefined) {
                        document.getElementById('overshoot').value = data.updated_overshoot;
                    }

                    document.getElementById('valSheets').textContent = data.total_sheets;
                    document.getElementById('valForms').textContent = data.total_forms;
                    document.getElementById('valTotalOvershoot').textContent = '+' + data.total_overshoot.toFixed(2) + '%';
                    document.getElementById('valOrdered').textContent = data.total_ordered;
                    document.getElementById('valProduced').textContent = data.total_produced;

                    const resultsContainer = document.getElementById('resultsContainer');
                    resultsContainer.innerHTML = '';

                    if (data.form_blocks && data.form_blocks.length > 0) {
                        data.form_blocks.forEach((block, index) => {
                            const subTitle = document.createElement('div');
                            subTitle.className = 'section-title';
                            subTitle.textContent = block.form_name;
                            resultsContainer.appendChild(subTitle);

                            const blockDiv = document.createElement('div');
                            blockDiv.className = 'code-block-container';

                            const codeBox = document.createElement('div');
                            codeBox.className = 'print-codes';
                            codeBox.textContent = block.code_line;

                            const copyBtn = document.createElement('button');
                            copyBtn.className = 'copy-btn';
                            copyBtn.textContent = 'Copy';
                            copyBtn.onclick = () => {
                                navigator.clipboard.writeText(block.code_line);
                                copyBtn.textContent = 'Copied!';
                                setTimeout(() => { copyBtn.textContent = 'Copy'; }, 2000);
                            };

                            blockDiv.appendChild(codeBox);
                            blockDiv.appendChild(copyBtn);
                            resultsContainer.appendChild(blockDiv);
                        });
                    }

                    const table = document.createElement('table');
                    let theadHTML = '<thead><tr>' +
                        '<th class="col-item">Item</th>' +
                        '<th class="col-quantity">Quantity</th>' +
                        '<th class="col-order">Order / Produced</th>' +
                        '<th class="col-overage">Overage</th>' +
                        '</tr></thead>';
                    let tbodyHTML = '<tbody>';
                    data.item_reports.forEach(item => {
                        let chipsHTML = '<div class="quantity-chips">';
                        if (item.slots_list && item.slots_list.length > 0) {
                            item.slots_list.forEach(val => {
                                chipsHTML += '<span class="quantity-chip">' + val + '</span>';
                            });
                        }
                        chipsHTML += '</div>';

                        tbodyHTML += '<tr>' +
                            '<td class="col-item"><b>№ ' + item.id + '</b></td>' +
                            '<td class="col-quantity">' + chipsHTML + '</td>' +
                            '<td class="col-order">' + item.target + ' / ' + item.produced + '</td>' +
                            '<td class="col-overage overshoot good">+' + item.overshoot.toFixed(2) + '%</td>' +
                            '</tr>';
                    });
                    tbodyHTML += '</tbody>';
                    table.innerHTML = theadHTML + tbodyHTML;
                    resultsContainer.appendChild(table);

                    reportPanel.style.display = 'block';
                }
            } catch (err) {
                errorBox.textContent = "Server connection error.";
                errorBox.style.display = 'block';
            } finally {
                btn.disabled = false;
            }
        }
    </script>
</body>
</html>`

type FormBlock struct {
    FormName string `json:"form_name"`
    CodeLine string `json:"code_line"`
}

type ExtendedCalcResponse struct {
    CalcResponse
    FormBlocks []FormBlock `json:"form_blocks"`
}

type MoveRequest struct {
    Dx int `json:"dx"`
    Dy int `json:"dy"`
}

//________________________________________________________
// Sets application window icon from settings/logo.png file on disk
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
// Main application entry point
func main() {
    execDir, err := os.Executable()
    if err == nil {
        baseDir := filepath.Dir(execDir)
        splashPath := filepath.Join(baseDir, "settings", "splash.exe")
        if _, err := os.Stat(splashPath); err == nil {
            cmd := exec.Command(splashPath, "500")
            cmd.Dir = filepath.Join(baseDir, "settings")
            _ = cmd.Start()
        }
    }

    go func() {
        http.HandleFunc("/", serveUI)
        http.HandleFunc("/api/calculate", handleCalc)
        http.HandleFunc("/api/close", handleClose)
        http.HandleFunc("/api/move", handleMove)
        _ = http.ListenAndServe(serverPort, nil)
    }()

    time.Sleep(200 * time.Millisecond)

    w := webview.New(false)
    defer w.Destroy()

    w.SetTitle("Layout-sheets-calc")

    setupFramelessWindow(w)

    var logoPath string
    if err == nil {
        logoPath = filepath.Join(filepath.Dir(execDir), "settings", "logo.png")
    } else {
        logoPath = filepath.Join("settings", "logo.png")
    }
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
// Native WebView2 frameless resizable window setup
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

//________________________________________________________
// Updates rounded window shape region according to current window size
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

    type rect struct {
        Left, Top, Right, Bottom int32
    }
    var rObj rect
    getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rObj)))

    width := int(rObj.Right - rObj.Left)
    height := int(rObj.Bottom - rObj.Top)

    hrgn, _, _ := createRoundRectRgn.Call(0, 0, uintptr(width), uintptr(height), 16, 16)
    if hrgn != 0 {
        setWindowRgn.Call(hwnd, hrgn, 1)
    }
}

//________________________________________________________
// Native OS drag logic via Win32 API
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
// Serves main HTML UI
func serveUI(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Write([]byte(indexHTML))
}

//________________________________________________________
// Handles application shutdown request
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
// Legacy HTTP drag API
func handleMove(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req MoveRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        sendJSON(w, CalcResponse{Success: false, Message: "Invalid JSON format"})
        return
    }

    if runtime.GOOS == "windows" {
        user32 := syscall.NewLazyDLL("user32.dll")
        getForegroundWindow := user32.NewProc("GetForegroundWindow")
        getWindowRect := user32.NewProc("GetWindowRect")
        setWindowPos := user32.NewProc("SetWindowPos")

        hwnd, _, _ := getForegroundWindow.Call()
        if hwnd != 0 {
            type rect struct {
                Left, Top, Right, Bottom int32
            }
            var rObj rect
            getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rObj)))

            const swpNoSize = 0x0001
            const swpNoZOrder = 0x0004
            const swpFrameChanged = 0x0020

            newX := int(rObj.Left) + req.Dx
            newY := int(rObj.Top) + req.Dy

            setWindowPos.Call(hwnd, 0, uintptr(newX), uintptr(newY), 0, 0, uintptr(swpNoSize|swpNoZOrder|swpFrameChanged))
        }
    }
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"success":true}`))
}

//________________________________________________________
// Handles layout calculation API endpoint
func handleCalc(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req CalcRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        sendJSON(w, CalcResponse{Success: false, Message: "Invalid JSON format"})
        return
    }

    parts := strings.Fields(req.Orders)
    var orders []int
    for _, p := range parts {
        qty, err := strconv.Atoi(p)
        if err == nil {
            orders = append(orders, qty)
        }
    }

    if len(orders) == 0 {
        sendJSON(w, CalcResponse{Success: false, Message: "No valid orders found."})
        return
    }

    totalSum := 0
    for _, o := range orders {
        totalSum += o
    }
    minSheets := int(math.Ceil(float64(totalSum) / float64(req.Capacity)))

    startOvershoot := req.OvershootPct
    var found bool
    var bestR []int
    var bestLayouts [][]int
    var finalOvershootPct = startOvershoot

    steps := []float64{startOvershoot}
    for s := math.Ceil(startOvershoot + 1); s <= 30.0; s += 1.0 {
        steps = append(steps, s)
    }
    for s := 35.0; s <= 100.0; s += 5.0 {
        steps = append(steps, s)
    }

searchOuter:
    for _, currentOvr := range steps {
        if currentOvr < startOvershoot {
            continue
        }
        maxOvershoot := currentOvr / 100.0

        for K := 1; K <= 2; K++ {
            for extraSheets := 0; extraSheets <= 20; extraSheets++ {
                totalSheets := minSheets + extraSheets
                partitions := getPartitions(totalSheets, K)

                for _, runs := range partitions {
                    layouts := solveExactCapacityDP(runs, orders, req.Capacity, maxOvershoot)
                    if layouts != nil {
                        bestR = runs
                        bestLayouts = layouts
                        found = true
                        finalOvershootPct = currentOvr
                        break searchOuter
                    }
                }
            }
        }
    }

    if !found {
        sendJSON(w, CalcResponse{Success: false, Message: "Could not find layout configuration even up to 100% overage limit."})
        return
    }

    resp := buildResponse(bestR, bestLayouts, orders, req.Capacity)
    resp.TotalOvershoot = ((float64(resp.TotalProduced) - float64(resp.TotalOrdered)) / float64(resp.TotalOrdered)) * 100.0
    resp.UpdatedOvershoot = finalOvershootPct

    if finalOvershootPct > startOvershoot {
        resp.Message = fmt.Sprintf("Overshoot automatically raised to %.0f%% to find a valid layout without zeros.", finalOvershootPct)
    }

    sendJSON(w, resp)
}

//________________________________________________________
// Constructs structured response payload
func buildResponse(R []int, layouts [][]int, orders []int, capacity int) ExtendedCalcResponse {
    totalSheets := 0
    for _, r := range R {
        totalSheets += r
    }

    var itemReports []ItemReport
    totalOrdered := 0
    totalProduced := 0

    var printCodes []string
    var formNames []string
    var formBlocks []FormBlock

    for j := 0; j < len(R); j++ {
        fName := fmt.Sprintf("Sheet %d (%d)", j+1, R[j])
        formNames = append(formNames, fName)
        var buffer bytes.Buffer
        for i := 0; i < len(orders); i++ {
            if layouts[i][j] > 0 {
                buffer.WriteString(fmt.Sprintf("%d*%d ", i+1, layouts[i][j]))
            }
        }
        line := strings.TrimSpace(buffer.String())
        printCodes = append(printCodes, line)
        formBlocks = append(formBlocks, FormBlock{
            FormName: fName,
            CodeLine: line,
        })
    }

    for i, order := range orders {
        produced := 0
        var slotsParts []string
        var slotsList []int
        for j := 0; j < len(R); j++ {
            slots := layouts[i][j]
            produced += slots * R[j]
            slotsParts = append(slotsParts, fmt.Sprintf("%d", slots))
            slotsList = append(slotsList, slots)
        }

        totalOrdered += order
        totalProduced += produced
        overshootPct := ((float64(produced) - float64(order)) / float64(order)) * 100.0

        itemReports = append(itemReports, ItemReport{
            ID:        i + 1,
            Target:    order,
            Produced:  produced,
            Overshoot: overshootPct,
            SlotsStr:  strings.Join(slotsParts, " | "),
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
        PrintCodes:     printCodes,
        FormNames:      formNames,
    }

    return ExtendedCalcResponse{
        CalcResponse: base,
        FormBlocks:   formBlocks,
    }
}

//________________________________________________________
// Helper function to send JSON response
func sendJSON(w http.ResponseWriter, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(data)
}

//________________________________________________________
// Generates integer partitions for sheets breakdown
func getPartitions(N, K int) [][]int {
    var res [][]int
    var backtrack func(partsLeft, target, minVal, maxVal int, current []int)
    backtrack = func(partsLeft, target, minVal, maxVal int, current []int) {
        if partsLeft == 1 {
            if target >= minVal && target <= maxVal {
                tmp := make([]int, len(current))
                copy(tmp, current)
                tmp = append(tmp, target)
                res = append(res, tmp)
            }
            return
        }
        for v := minVal; v <= target-(partsLeft-1); v++ {
            tmp := make([]int, len(current))
            copy(tmp, current)
            tmp = append(tmp, v)
            backtrack(partsLeft-1, target-v, minVal, maxVal, tmp)
        }
    }
    backtrack(K, N, 1, N, []int{})
    return res
}

//________________________________________________________
// Encodes capacity slice into bit-packed State type
func encode(caps []int) State {
    var s State
    for i, c := range caps {
        s |= State(c) << (9 * i)
    }
    return s
}

//________________________________________________________
// Decodes bit-packed State back into capacity slice
func decode(s State, K int) []int {
    caps := make([]int, K)
    for i := 0; i < K; i++ {
        caps[i] = int((s >> (9 * i)) & 0x1FF)
    }
    return caps
}

//________________________________________________________
// DP solver for exact capacity layouts
func solveExactCapacityDP(R []int, orders []int, capacity int, maxOvr float64) [][]int {
    K := len(R)
    validTuples := make([][][]int, len(orders))

    for i, order := range orders {
        minReq := order
        maxReq := int(math.Floor(float64(order) * (1.0 + maxOvr)))

        var backtrack func(dim, currentSum int, currentTuple []int)
        backtrack = func(dim, currentSum int, currentTuple []int) {
            if dim == K {
                if currentSum >= minReq && currentSum <= maxReq {
                    allZeros := true
                    for _, val := range currentTuple {
                        if val > 0 {
                            allZeros = false
                            break
                        }
                    }
                    if !allZeros {
                        tmp := make([]int, K)
                        copy(tmp, currentTuple)
                        validTuples[i] = append(validTuples[i], tmp)
                    }
                }
                return
            }
            maxS := maxReq / R[dim]
            if maxS > capacity {
                maxS = capacity
            }
            minS := 1
            for s := minS; s <= maxS; s++ {
                currentTuple[dim] = s
                backtrack(dim+1, currentSum+s*R[dim], currentTuple)
            }
        }
        backtrack(0, 0, make([]int, K))

        if len(validTuples[i]) == 0 {
            return nil
        }
    }

    maxSlotsRemaining := make([][]int, len(orders))
    for i := len(orders) - 1; i >= 0; i-- {
        maxSlotsRemaining[i] = make([]int, K)
        for j := 0; j < K; j++ {
            maxAllowed := 0
            for _, t := range validTuples[i] {
                if t[j] > maxAllowed {
                    maxAllowed = t[j]
                }
            }
            if i < len(orders)-1 {
                maxSlotsRemaining[i][j] = maxSlotsRemaining[i+1][j] + maxAllowed
            } else {
                maxSlotsRemaining[i][j] = maxAllowed
            }
        }
    }

    targetCaps := mainTargetCaps(K, capacity)
    targetState := encode(targetCaps)

    prevStates := make(map[State]int)
    prevStates[0] = -1
    history := make([]map[State]int, len(orders))

    for i := 0; i < len(orders); i++ {
        nextStates := make(map[State]int)
        for state := range prevStates {
            caps := decode(state, K)

            for choiceIdx, tuple := range validTuples[i] {
                valid := true
                newCaps := make([]int, K)

                for j := 0; j < K; j++ {
                    newCaps[j] = caps[j] + tuple[j]
                    if newCaps[j] > capacity {
                        valid = false
                        break
                    }

                    remCap := capacity - newCaps[j]
                    var maxRem int
                    if i < len(orders)-1 {
                        maxRem = maxSlotsRemaining[i+1][j]
                    }
                    if remCap > maxRem {
                        valid = false
                        break
                    }
                }

                if valid {
                    ns := encode(newCaps)
                    if _, exists := nextStates[ns]; !exists {
                        nextStates[ns] = choiceIdx
                    }
                }
            }
        }
        if len(nextStates) == 0 {
            return nil
        }
        history[i] = nextStates
        prevStates = nextStates
    }

    if _, exists := prevStates[targetState]; !exists {
        return nil
    }

    ans := make([][]int, len(orders))
    currState := targetState
    for i := len(orders) - 1; i >= 0; i-- {
        choiceIdx := history[i][currState]
        tuple := validTuples[i][choiceIdx]
        ans[i] = tuple

        caps := decode(currState, K)
        for j := 0; j < K; j++ {
            caps[j] -= tuple[j]
        }
        currState = encode(caps)
    }
    return ans
}

//________________________________________________________
// Returns target capacities slice for DP solver
func mainTargetCaps(K, capacity int) []int {
    t := make([]int, K)
    for i := 0; i < K; i++ {
        t[i] = capacity
    }
    return t
}