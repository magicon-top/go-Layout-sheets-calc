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
type OrderItem struct {
	PageNum  int
	Quantity int
}

//________________________________________________________
type CalcRequest struct {
	OvershootPct float64 `json:"overshoot"`
	Capacity     int     `json:"capacity"`
	Orders       string  `json:"orders"`
}

//________________________________________________________
type ItemReport struct {
	PageNum   int     `json:"page_num"`
	Target    int     `json:"target"`
	Produced  int     `json:"produced"`
	Overshoot float64 `json:"overshoot"`
	SlotsStr  string  `json:"slots_str"`
	SlotsList []int   `json:"slots_list"`
}

//________________________________________________________
type CalcResponse struct {
	Success          bool         `json:"success"`
	Message          string       `json:"message"`
	TotalSheets      int          `json:"total_sheets"`
	TotalForms       int          `json:"total_forms"`
	Forms            []int        `json:"forms"`
	ItemReports      []ItemReport `json:"item_reports"`
	TotalOrdered     int          `json:"total_ordered"`
	TotalProduced    int          `json:"total_produced"`
	TotalOvershoot   float64      `json:"total_overshoot"`
	PrintCodes       []string     `json:"print_codes"`
	FormNames        []string     `json:"form_names"`
	UpdatedOvershoot float64      `json:"updated_overshoot"`
}

//________________________________________________________
const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Layout-sheets-calc.Magicon.top by Levchuk</title>
    <style>
        html, body { height: 100%; width: 100%; background-color: #f0f2f5; margin: 0; padding: 0; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; color: #333; font-size: 14px; overflow: hidden; -webkit-user-select: none; user-select: none; box-sizing: border-box; }
        .titlebar { background: #EFF9DE; color: #432818; height: 28px; display: flex; align-items: center; justify-content: space-between; padding: 0 0 0 8px; box-sizing: border-box; cursor: default; flex-shrink: 0; }
        .titlebar-title { font-size: 12px; font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; pointer-events: none; }
        .titlebar-controls { display: flex; align-items: center; gap: 0; height: 100%; padding-right: 4px; }

        .container { width: 100%; height: calc(100% - 28px); box-sizing: border-box; position: relative; padding: 4px; display: flex; flex-direction: column; overflow-y: auto;  overflow-y: scroll; }

        .header-panel { background: white; padding: 6px 8px; border-radius: 4px; box-shadow: 0 1px 3px rgba(0,0,0,0.05); width: 100%; box-sizing: border-box; position: relative; flex-shrink: 0; }
        .top-row { display: flex; gap: 8px; align-items: flex-end; margin-bottom: 4px; }
        .input-group { display: flex; flex-direction: column; gap: 2px; flex-shrink: 0; }
        .input-group.full-width { flex: 1; min-width: 0; }
        .input-group label { font-size: 11px; font-weight: bold; color: #555; white-space: nowrap; }
        .input-group input, .input-group textarea { padding: 3px 6px; border: 1px solid #ccc; border-radius: 3px; font-size: 14px; font-weight: bold; outline: none; transition: border 0.2s; box-sizing: border-box; font-family: inherit; -webkit-user-select: text; user-select: text; }
        .input-group input:focus, .input-group textarea:focus { border-color: #007bff; }

        #capacity { 
            width: 70px; 
            height: 28px; 
            font-size: 21px; 
            color: #006400; 
            font-weight: bold;
        }
        #orders { width: 100%; min-height: 28px; resize: none; overflow-y: hidden; line-height: 1.3; box-sizing: border-box; }

        .action-buttons {
            position: absolute;
            right: 6px;
            top: 6px;
            display: flex;
            flex-direction: row;
            gap: 4px;
            z-index: 100;
        }
        .action-btn {
            padding: 0 10px;
            border: none;
            border-radius: 3px;
            cursor: pointer;
            transition: background 0.2s;
            height: 28px;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 12px;
            font-weight: bold;
            color: white;
            width: auto;
            min-width: 50px;
        }
        .action-btn svg {
            width: 16px;
            height: 16px;
            fill: white;
        }

        #calcBtn { background: #007bff; }
        #calcBtn:hover:not(:disabled) { background: #0056b3; }
        #calcBtn:disabled { background: #6c757d; cursor: not-allowed; opacity: 0.65; }

        #reduceBtn { background: #dc3545; }
        #reduceBtn:hover:not(:disabled) { background: #b02a37; }
        #reduceBtn:disabled { background: #6c757d; cursor: not-allowed; opacity: 0.65; }

        #increaseBtn { background: #28a745; }
        #increaseBtn:hover:not(:disabled) { background: #1e7e34; }
        #increaseBtn:disabled { background: #6c757d; cursor: not-allowed; opacity: 0.65; }

        button.titlebar-btn { padding: 0; border: none; background: transparent; cursor: pointer; width: 28px; height: 22px; border-radius: 3px; display: flex; align-items: center; justify-content: center; transition: background 0.2s; }
        button.titlebar-btn.close { background: transparent; border-radius: 3px; width: 24px; height: 22px; transition: background 0.2s, fill 0.2s; }
        button.titlebar-btn.close:hover { background: #dc3545; }
        button.titlebar-btn svg { width: 12px; height: 12px; fill: #333333; stroke: #333333; stroke-width: 0; display: block; margin: auto; transition: fill 0.2s; }
        button.titlebar-btn.close svg { fill: #333333; stroke: #333333; }
        button.titlebar-btn.close:hover svg { fill: white; stroke: white; }

        .report-panel { margin-top: 4px; background: white; padding: 6px 8px; border-radius: 4px; box-shadow: 0 1px 3px rgba(0,0,0,0.05); display: none; width: 100%; box-sizing: border-box; }

        .summary-cards { display: flex; gap: 4px; margin-bottom: 6px; flex-wrap: nowrap; width: 100%; box-sizing: border-box; }
        .card { background: #f8f9fa; padding: 4px 6px; border-radius: 3px; border-left: 2px solid #007bff; flex: 1; min-width: 0; box-sizing: border-box; }
        .card.warning { border-left-color: #ffc107; }
        .card.success { border-left-color: #2d2d2d; }
        .card-title { font-size: 9px; color: #666; text-transform: uppercase; letter-spacing: 0.3px; margin-bottom: 1px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .card-value { font-size: 14px; font-weight: bold; color: #222; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

        .card-value.forms-value {
            color: #dc3545;
            font-size: 26px;
            font-weight: bold;
        }

        .section-title { font-size: 14px; font-weight: bold; margin: 4px 0 2px 0; color: #333; display: flex; align-items: center; gap: 4px; }
        .sheet-badge { background-color: #800020; color: #ffffff; padding: 1px 4px; border-radius: 3px; font-weight: bold; display: inline-block; }

        .code-block-container { margin-bottom: 4px; width: 100%; box-sizing: border-box; display: flex; align-items: stretch; gap: 4px; }
        .print-codes { color: #ffffff; background: #2d2d2d; padding: 6px 10px; border-radius: 3px; font-family: monospace; white-space: normal; word-break: break-all; overflow-wrap: break-word; font-size: 14px; flex-grow: 1; box-sizing: border-box; border: 1px solid #2d2d2d; display: flex; align-items: center; -webkit-user-select: text; user-select: text; }
        .copy-btn { padding: 0 10px; background: #6c757d; color: white; border: none; border-radius: 3px; font-size: 13px; cursor: pointer; height: auto; display: inline-flex; align-items: center; justify-content: center; flex-shrink: 0; font-weight: bold; }

        table { width: 100%; border-collapse: collapse; margin-bottom: 4px; -webkit-user-select: text; user-select: text; table-layout: fixed; }
        th.col-item, td.col-item { width: 60px; }
        th.col-overage, td.col-overage { width: 70px; text-align: right; }
        th.col-order, td.col-order { width: 80px; text-align: right; }
        th.col-quantity, td.col-quantity { width: auto; }

        th, td { padding: 3px 6px; text-align: left; border-bottom: 1px solid #eee; font-size: 14px; vertical-align: middle; }
        th { background-color: #f8f9fa; font-weight: 600; color: #444; padding-top: 3px; padding-bottom: 3px; }
        tr:hover { background-color: #f1f4f8; }
        .overshoot { font-weight: bold; }
        .overshoot.good { color: #28a745; }

        .quantity-chips { display: flex; flex-wrap: wrap; gap: 3px; align-items: center; }
        .quantity-chip { background-color: #2d2d2d; color: #ffffff; padding: 1px 3px; border-radius: 3px; min-width: 30px; text-align: center; font-size: 13px; font-weight: 600; display: inline-block; box-sizing: border-box; }

        .error { color: #dc3545; font-weight: bold; padding: 6px; background: #ffe6e6; border-radius: 3px; display: none; margin-top: 4px; width: 100%; box-sizing: border-box; font-size: 13px; }
    </style>
</head>
<body>
    <div class="titlebar" id="titleBar">
        <div class="titlebar-title">Layout-sheets-calc.MagicON.Top by Levchuk V.N.</div>
        <div class="titlebar-controls">
            <button class="titlebar-btn close" onclick="closeApp()" title="Close Application">
                <svg viewBox="0 0 24 24"><path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>
            </button>
        </div>
    </div>

    <div class="container">
        <div class="action-buttons">
            <button id="calcBtn" class="action-btn" onclick="calculate()" title="Calculate">Calc</button>
            <button id="reduceBtn" class="action-btn" onclick="reduceSheets()" title="Reduce Sheets by 1">-1 Sheet</button>
            <button id="increaseBtn" class="action-btn" onclick="increaseSheets()" title="Increase Sheets by 2">+1 Sheet</button>
        </div>

        <div class="header-panel">
            <div class="top-row">
                <div class="input-group" style="flex-shrink: 0;">
                    <label>Capacity</label>
                    <input type="number" id="capacity" value="88" min="1">
                </div>
                <div class="input-group full-width">
                    <label>Orders (space separated)</label>
                    <textarea id="orders" rows="1" oninput="autoResize(this)">11*1000 14*500 15*500 23*250 25*1000 28*500 33*2000 50*500 60*250 71*250 74*250 93*500 94*500 103*2000 113*500 117*250 119*1000 120*250 122*500 139*250 155*500 157*250 162*500 169*500 171*1000 172*250 188*250 191*750 193*500 199*500 205*500 206*500 221*1000 224*1000 227*250 234*1000 235*500 236*1000 238*500 242*1000 243*1000 253*1000 256*1000</textarea>
                </div>
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
                    <div class="card-value forms-value" id="valForms">-</div>
                </div>
                <div class="card warning">
                    <div class="card-title">Total Overage</div>
                    <div class="card-value" id="valTotalOvershoot">-</div>
                </div>
                <div class="card success" style="border-left-color: #2d2d2d; background: #2d2d2d;">
                    <div class="card-title" style="color: #cccccc;">Ordered / Produced</div>
                    <div class="card-value" style="font-size: 13px; margin-top: 1px; color: #ffffff;"><span id="valOrdered"></span> / <span id="valProduced"></span></div>
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

        // Глобальные переменные
        let currentOvershoot = 0;
        let overshootHistory = [];

        // Общая функция для вызова API расчёта
        async function fetchCalculation(payload) {
            const response = await fetch('/api/calculate', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });
            return await response.json();
        }

        // Обновляет состояние кнопок и скрывает/показывает панель
        function updateButtonsState(currentForms, minSheets, disableAll, hideReport) {
            const reduceBtn = document.getElementById('reduceBtn');
            const increaseBtn = document.getElementById('increaseBtn');
            const calcBtn = document.getElementById('calcBtn');
            const reportPanel = document.getElementById('reportPanel');

            // Скрываем панель, если hideReport = true
            if (hideReport) {
                reportPanel.style.display = 'none';
            }

            // Calc всегда активна
            calcBtn.disabled = false;

            // Если пришёл флаг отключения всех кнопок (изменение полей)
            if (disableAll) {
                reduceBtn.disabled = true;
                increaseBtn.disabled = true;
                return;
            }

            // -1 Sheet: активна, если currentForms > minSheets
            if (currentForms <= minSheets) {
                reduceBtn.disabled = true;
                reduceBtn.title = 'Already at minimum sheets';
            } else {
                reduceBtn.disabled = false;
                reduceBtn.title = 'Reduce Sheets by 1';
            }

            // +1 Sheet: активна, если в истории есть элементы
            if (overshootHistory.length > 0) {
                increaseBtn.disabled = false;
                increaseBtn.title = 'Revert to previous overshoot';
            } else {
                increaseBtn.disabled = true;
                increaseBtn.title = 'No history to revert';
            }
        }

        // При изменении полей orders или capacity отключаем кнопки +1 и -1, скрываем панель
        document.getElementById('orders').addEventListener('input', function() {
            overshootHistory = [];
            currentOvershoot = 0;
            const currentForms = parseInt(document.getElementById('valForms').textContent) || 0;
            const parts = document.getElementById('orders').value.trim().split(/\s+/).filter(s => s.length > 0);
            const numItems = parts.length;
            const capacityVal = parseInt(document.getElementById('capacity').value) || 88;
            const minSheets = Math.ceil(numItems / capacityVal);
            updateButtonsState(currentForms, minSheets, true, true);
        });
        
        document.getElementById('capacity').addEventListener('change', function() {
            overshootHistory = [];
            currentOvershoot = 0;
            const currentForms = parseInt(document.getElementById('valForms').textContent) || 0;
            const parts = document.getElementById('orders').value.trim().split(/\s+/).filter(s => s.length > 0);
            const numItems = parts.length;
            const capacityVal = parseInt(document.getElementById('capacity').value) || 88;
            const minSheets = Math.ceil(numItems / capacityVal);
            updateButtonsState(currentForms, minSheets, true, true);
        });
        document.getElementById('capacity').addEventListener('input', function() {
            overshootHistory = [];
            currentOvershoot = 0;
            const currentForms = parseInt(document.getElementById('valForms').textContent) || 0;
            const parts = document.getElementById('orders').value.trim().split(/\s+/).filter(s => s.length > 0);
            const numItems = parts.length;
            const capacityVal = parseInt(document.getElementById('capacity').value) || 88;
            const minSheets = Math.ceil(numItems / capacityVal);
            updateButtonsState(currentForms, minSheets, true, true);
        });

        // Основной расчёт – обновляет UI
        async function calculate() {
            const calcBtn = document.getElementById('calcBtn');
            const reduceBtn = document.getElementById('reduceBtn');
            const increaseBtn = document.getElementById('increaseBtn');
            const errorBox = document.getElementById('errorBox');
            const reportPanel = document.getElementById('reportPanel');

            calcBtn.disabled = true;
            reduceBtn.disabled = true;
            increaseBtn.disabled = true;
            errorBox.style.display = 'none';
            reportPanel.style.display = 'none';

            const payload = {
                overshoot: currentOvershoot,
                capacity: parseInt(document.getElementById('capacity').value) || 88,
                orders: document.getElementById('orders').value
            };

            try {
                const data = await fetchCalculation(payload);

                if (!data.success) {
                    errorBox.textContent = data.message;
                    errorBox.style.display = 'block';
                } else {
                    document.getElementById('valSheets').textContent = data.total_sheets;
                    document.getElementById('valForms').textContent = data.total_forms;
                    document.getElementById('valTotalOvershoot').textContent = '+' + data.total_overshoot.toFixed(2) + '%';
                    document.getElementById('valOrdered').textContent = data.total_ordered;
                    document.getElementById('valProduced').textContent = data.total_produced;

                    const parts = document.getElementById('orders').value.trim().split(/\s+/).filter(s => s.length > 0);
                    const numItems = parts.length;
                    const capacityVal = parseInt(document.getElementById('capacity').value) || 88;
                    const minSheets = Math.ceil(numItems / capacityVal);
                    updateButtonsState(data.total_forms, minSheets, false, false);
                    reportPanel.style.display = 'block';

                    const resultsContainer = document.getElementById('resultsContainer');
                    resultsContainer.innerHTML = '';

                    if (data.form_blocks && data.form_blocks.length > 0) {
                        data.form_blocks.forEach((block, index) => {
                            const subTitle = document.createElement('div');
                            subTitle.className = 'section-title';
                            subTitle.innerHTML = block.form_name_html;
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
                        '<th class="col-item">Page</th>' +
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
                            '<td class="col-item"><b>№ ' + item.page_num + '</b></td>' +
                            '<td class="col-quantity">' + chipsHTML + '</td>' +
                            '<td class="col-order">' + item.target + ' / ' + item.produced + '</td>' +
                            '<td class="col-overage overshoot good">+' + item.overshoot.toFixed(2) + '%</td>' +
                            '</tr>';
                    });
                    tbodyHTML += '</tbody>';
                    table.innerHTML = theadHTML + tbodyHTML;
                    resultsContainer.appendChild(table);

                    if (data.logs && data.logs.length > 0) {
                        const logsTitle = document.createElement('div');
                        logsTitle.className = 'section-title';
                        logsTitle.style.marginTop = '8px';
                        logsTitle.textContent = 'Calculation Cycle Logs';
                        resultsContainer.appendChild(logsTitle);

                        const logsDiv = document.createElement('div');
                        logsDiv.className = 'code-block-container';

                        const logsPre = document.createElement('pre');
                        logsPre.style.cssText = 'background: #1e1e1e; color: #a5d6ff; padding: 6px; border-radius: 3px; overflow-x: auto; font-size: 11px; margin: 0; width: 100%; box-sizing: border-box; font-family: Consolas, monospace;';
                        logsPre.textContent = data.logs;

                        logsDiv.appendChild(logsPre);
                        resultsContainer.appendChild(logsDiv);
                    }

                    reportPanel.style.display = 'block';
                }
            } catch (err) {
                errorBox.textContent = "Server connection error.";
                errorBox.style.display = 'block';
            } finally {
                const currentForms = parseInt(document.getElementById('valForms').textContent) || 0;
                const parts2 = document.getElementById('orders').value.trim().split(/\s+/).filter(s => s.length > 0);
                const numItems2 = parts2.length;
                const capacityVal2 = parseInt(document.getElementById('capacity').value) || 88;
                const minSheets2 = Math.ceil(numItems2 / capacityVal2);
                updateButtonsState(currentForms, minSheets2, false, false);
                calcBtn.disabled = false;
            }
        }

        // -1 Sheet: уменьшаем количество уникальных листов на 1 (ищем минимальный overshoot)
        async function reduceSheets() {
            const reduceBtn = document.getElementById('reduceBtn');
            const increaseBtn = document.getElementById('increaseBtn');
            const calcBtn = document.getElementById('calcBtn');
            const errorBox = document.getElementById('errorBox');

            if (reduceBtn.disabled) return;

            // Запоминаем текущий overshoot в историю перед поиском
            overshootHistory.push(currentOvershoot);

            reduceBtn.disabled = true;
            increaseBtn.disabled = true;
            calcBtn.disabled = true;
            errorBox.style.display = 'none';

            const capacity = parseInt(document.getElementById('capacity').value) || 88;
            const orders = document.getElementById('orders').value;

            reduceBtn.textContent = 'Searching...';

            try {
                const initialPayload = { overshoot: currentOvershoot, capacity, orders };
                const initialData = await fetchCalculation(initialPayload);
                if (!initialData.success) {
                    errorBox.textContent = initialData.message || 'Calculation error';
                    errorBox.style.display = 'block';
                    overshootHistory.pop();
                    return;
                }
                const startTotalForms = initialData.total_forms;
                const parts = orders.trim().split(/\s+/).filter(s => s.length > 0);
                const numItems = parts.length;
                const minSheets = Math.ceil(numItems / capacity);

                if (startTotalForms <= minSheets) {
                    errorBox.textContent = 'Already at minimum sheets.';
                    errorBox.style.display = 'block';
                    overshootHistory.pop();
                    updateButtonsState(startTotalForms, minSheets, false, false);
                    return;
                }

                let newOvershoot = currentOvershoot + 1;
                let found = false;

                while (newOvershoot <= 10000) {
                    const payload = { overshoot: newOvershoot, capacity, orders };
                    const data = await fetchCalculation(payload);
                    if (!data.success) {
                        errorBox.textContent = data.message || 'Calculation error';
                        errorBox.style.display = 'block';
                        break;
                    }
                    if (data.total_forms < startTotalForms) {
                        currentOvershoot = newOvershoot;
                        found = true;
                        break;
                    }
                    newOvershoot += 1;
                }

                if (!found) {
                    errorBox.textContent = 'Cannot reduce sheets further with current algorithm.';
                    errorBox.style.display = 'block';
                    reduceBtn.disabled = true;
                    overshootHistory.pop();
                    await calculate();
                    return;
                }

                await calculate();

            } catch (err) {
                errorBox.textContent = "Error during reduction process.";
                errorBox.style.display = 'block';
                overshootHistory.pop();
            } finally {
                reduceBtn.textContent = '-1 Sheet';
                const currentForms = parseInt(document.getElementById('valForms').textContent) || 0;
                const parts2 = orders.trim().split(/\s+/).filter(s => s.length > 0);
                const numItems2 = parts2.length;
                const minSheets2 = Math.ceil(numItems2 / capacity);
                updateButtonsState(currentForms, minSheets2, false, false);
                calcBtn.disabled = false;
            }
        }

        // +1 Sheet: откат к предыдущему значению overshoot из истории
        async function increaseSheets() {
            const increaseBtn = document.getElementById('increaseBtn');
            if (increaseBtn.disabled) return;

            if (overshootHistory.length === 0) return;

            currentOvershoot = overshootHistory.pop();
            await calculate();
        }

        // Остальные функции (closeApp, autoResize, события)
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

        async function closeApp() {
            if (window.closeAppNative) {
                window.closeAppNative();
            } else {
                try { await fetch('/api/close', { method: 'POST' }); } catch (e) {}
                window.close();
            }
        }
    </script>
</body>
</html>`

//________________________________________________________
type FormBlock struct {
	FormName     string `json:"form_name"`
	FormNameHtml string `json:"form_name_html"`
	CodeLine     string `json:"code_line"`
}

//________________________________________________________
type ExtendedCalcResponse struct {
	CalcResponse
	FormBlocks []FormBlock `json:"form_blocks"`
	Logs       string      `json:"logs"`
}

//________________________________________________________
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

	// Сначала делаем окно безрамочным
	setupFramelessWindow(w)

	// Теперь изменяем размер и позицию: Y=0, высота 95% экрана, X не трогаем
	if runtime.GOOS == "windows" {
		user32 := syscall.NewLazyDLL("user32.dll")
		getSystemMetrics := user32.NewProc("GetSystemMetrics")
		
		// Получаем высоту экрана
		screenHeight, _, _ := getSystemMetrics.Call(1) // SM_CYSCREEN = 1
		if screenHeight > 0 {
			newHeight := int(float64(screenHeight) * 0.95)
			
			// Получаем текущую позицию и размер окна
			getWindowRect := user32.NewProc("GetWindowRect")
			var rect struct {
				Left, Top, Right, Bottom int32
			}
			hwnd := uintptr(w.Window())
			getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
			
			// Оставляем X как есть (не меняем), Y ставим 0
			currentX := rect.Left
			currentWidth := rect.Right - rect.Left
			
			// Устанавливаем новую позицию и размер
			setWindowPos := user32.NewProc("SetWindowPos")
			const SWP_NOZORDER = 0x0004
			
			setWindowPos.Call(
				hwnd,
				0,
				uintptr(currentX), // X не меняем
				uintptr(0),        // Y = 0 (самый верх)
				uintptr(currentWidth),
				uintptr(newHeight),
				uintptr(SWP_NOZORDER),
			)
			// Обновляем скруглённую область
			updateRoundedRegion(w)
		}
	}

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
// Uses Greedy allocation to pack capacity per sheet intelligently
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

	iteration := 1
	for {
		var remaining []int
		for i := 0; i < M; i++ {
			if produced[i] < items[i].Quantity {
				remaining = append(remaining, i)
			}
		}

		if len(remaining) == 0 {
			logs = append(logs, fmt.Sprintf("\nDONE: All %d unique items successfully placed on sheets.", M))
			break
		}

		if iteration > 1000 {
			logs = append(logs, "\nSAFETY STOP: Reached 1000 calculation cycles.")
			break
		}

		logs = append(logs, fmt.Sprintf("\n--- CYCLE %d ---", iteration))
		logs = append(logs, fmt.Sprintf("Items awaiting production: %d", len(remaining)))

		slots := make([]int, M)
		unallocated := capacity

		// 1. PRE-ALLOCATION: Ensure at least 1 slot per remaining item if capacity allows
		if unallocated >= len(remaining) {
			for _, i := range remaining {
				slots[i] = 1
				unallocated--
			}
			logs = append(logs, "Pre-allocated 1 slot to all remaining items.")
		} else {
			logs = append(logs, "Warning: Capacity is less than remaining items. Some items will wait for next sheet.")
		}

		// 2. D'Hondt apportionment (Distributes available slots proportionally to quantity left)
		for unallocated > 0 {
			bestItem := -1
			bestScore := -1.0
			for _, i := range remaining {
				need := items[i].Quantity - produced[i]
				maxAllowedQty := maxAllowed[i]
				maxRemainingOvr := maxAllowedQty - produced[i]

				// Do not allocate a slot if even 1 run (R=1) would violate overshoot
				if slots[i]+1 > maxRemainingOvr {
					continue
				}

				score := float64(need) / float64(slots[i]+1)
				if score > bestScore {
					bestScore = score
					bestItem = i
				}
			}
			if bestItem != -1 {
				slots[bestItem]++
				unallocated--
			} else {
				logs = append(logs, "Stopped allocation early: overshoot constraints prevent filling remaining slots.")
				break
			}
		}

		var allocLog []string
		for i, s := range slots {
			if s > 0 {
				need := items[i].Quantity - produced[i]
				allocLog = append(allocLog, fmt.Sprintf("[Pg %d : need %d → %d slots]", items[i].PageNum, need, s))
			}
		}
		logs = append(logs, fmt.Sprintf("Slot distribution (%d places max):", capacity))
		logs = append(logs, "  "+strings.Join(allocLog, ", "))

		// 3. Calculate Run length (R) based on overshoot limits
		rLowerBound := 0
		rUpperBound := math.MaxInt32

		for i, s := range slots {
			if s > 0 {
				need := items[i].Quantity - produced[i]

				// Minimum runs to finish this item on this sheet
				reqMin := int(math.Ceil(float64(need) / float64(s)))
				if reqMin > rLowerBound {
					rLowerBound = reqMin
				}

				// Maximum runs before violating the global overshoot setting
				maxAllowedQty := maxAllowed[i]
				maxRemainingOvr := maxAllowedQty - produced[i]
				maxAllowedR := maxRemainingOvr / s

				if maxAllowedR < rUpperBound {
					rUpperBound = maxAllowedR
				}
			}
		}

		R := 1
		if rLowerBound <= rUpperBound {
			// We can finish all allocated items without exceeding overshoot
			R = rLowerBound
			logs = append(logs, fmt.Sprintf("Can complete all allocated items. R = %d", R))
		} else {
			// We can't finish all. Print max possible without overshoot violation
			R = rUpperBound
			if R <= 0 {
				R = 1
			}
			logs = append(logs, fmt.Sprintf("Cannot complete all items perfectly. Bottleneck limits R to %d", R))
		}

		// Apply production results for this form
		for i, s := range slots {
			if s > 0 {
				produced[i] += R * s
			}
		}

		forms = append(forms, slots)
		runs = append(runs, R)
		iteration++
	}

	// Transpose layout matrix
	layouts := make([][]int, M)
	for i := 0; i < M; i++ {
		layouts[i] = make([]int, len(forms))
		for j := 0; j < len(forms); j++ {
			layouts[i][j] = forms[j][i]
		}
	}

	return layouts, runs, logs
}

//________________________________________________________
// Handles layout calculation API endpoint using new Heuristic Grouping
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
	var items []OrderItem
	var orders []int
	for _, p := range parts {
		subParts := strings.Split(p, "*")
		if len(subParts) == 2 {
			pageNum, err1 := strconv.Atoi(subParts[0])
			qty, err2 := strconv.Atoi(subParts[1])
			if err1 == nil && err2 == nil && qty > 0 {
				items = append(items, OrderItem{PageNum: pageNum, Quantity: qty})
				orders = append(orders, qty)
			}
		}
	}

	if len(orders) == 0 {
		sendJSON(w, CalcResponse{Success: false, Message: "No valid orders found."})
		return
	}

	// Heuristic distribution replaces heavy combinations array
	bestLayouts, bestR, traceLogs := calculateHeuristic(items, req.Capacity, req.OvershootPct)
	logsStr := strings.Join(traceLogs, "\n")

	if len(bestR) == 0 {
		sendJSON(w, ExtendedCalcResponse{
			CalcResponse: CalcResponse{
				Success: false,
				Message: "Could not find layout configuration.",
			},
			Logs: logsStr,
		})
		return
	}

	resp := buildResponse(bestR, bestLayouts, items, req.Capacity)
	resp.TotalOvershoot = ((float64(resp.TotalProduced) - float64(resp.TotalOrdered)) / float64(resp.TotalOrdered)) * 100.0
	resp.UpdatedOvershoot = req.OvershootPct
	resp.Logs = logsStr

	sendJSON(w, resp)
}

//________________________________________________________
// Constructs structured response payload
func buildResponse(R []int, layouts [][]int, items []OrderItem, capacity int) ExtendedCalcResponse {
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
		fName := fmt.Sprintf("Sheet %d %d", j+1, R[j])
		fNameHtml := fmt.Sprintf("Sheet %d <span class=\"sheet-badge\">%d</span> Pcs", j+1, R[j])
		formNames = append(formNames, fName)
		var buffer bytes.Buffer
		for i := 0; i < len(items); i++ {
			if layouts[i][j] > 0 {
				buffer.WriteString(fmt.Sprintf("%d*%d ", items[i].PageNum, layouts[i][j]))
			}
		}
		line := strings.TrimSpace(buffer.String())
		printCodes = append(printCodes, line)
		formBlocks = append(formBlocks, FormBlock{
			FormName:     fName,
			FormNameHtml: fNameHtml,
			CodeLine:     line,
		})
	}

	for i, item := range items {
		produced := 0
		var slotsParts []string
		var slotsList []int
		for j := 0; j < len(R); j++ {
			slots := layouts[i][j]
			produced += slots * R[j]
			slotsParts = append(slotsParts, fmt.Sprintf("%d", slots))
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