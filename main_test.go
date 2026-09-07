package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	pluginapi "orby/plugins"
)

func TestComposerCSSKeepsResponsiveControlsReadable(t *testing.T) {
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, rule := range []string{
		".operator-composer .composer-input, .operator-composer .composer-select {",
		"flex: 1 1 0;",
		"padding: 0 28px 0 9px;",
		"overflow-x: auto;",
		".terminal-pane { min-width: 0; }",
	} {
		if !strings.Contains(css, rule) {
			t.Fatalf("responsive stylesheet is missing %q", rule)
		}
	}
	if strings.Contains(css, ".terminal-pane { min-width: 600px; }") {
		t.Fatal("terminal pane forces page-level overflow on narrow screens")
	}
	if strings.Contains(css, ".composer-input {\n  padding-right:") {
		t.Fatal("input-specific padding makes editable controls different widths")
	}
	if !strings.Contains(css, "@media (max-width: 1200px) {") || !strings.Contains(css, "grid-template-columns: 38px minmax(260px, 1fr) 38px minmax(var(--run-btn-min), auto);") {
		t.Fatal("composer does not compact when both desktop sidebars are open")
	}
}

func TestOutputCardsUseCompactGap(t *testing.T) {
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	if !strings.Contains(css, "#output-area { display: grid; gap: 8px; min-width: 0; }") || !strings.Contains(css, "margin: 0;") {
		t.Fatal("output cards do not use one compact 8px gap")
	}
	if strings.Contains(css, "margin: 8px 0 18px 18px;") {
		t.Fatal("output cards still add their own large vertical margins")
	}
}

func TestVirtualOutputFillsCardWidth(t *testing.T) {
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	if !strings.Contains(css, "width: 100%;\n  max-width: 100%;") {
		t.Fatal("virtual output viewport does not fill the result card")
	}
	for _, expected := range []string{"#output-area { display: grid; gap: 8px; min-width: 0; }", ".cmd-block {\n  min-width: 0;", ".cmd-output { min-width: 0;"} {
		if !strings.Contains(css, expected) {
			t.Fatalf("resizable output containment is missing %q", expected)
		}
	}
	script, err := os.ReadFile("static/output_view.mjs")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		"const minimumColumnWidth = 180",
		"Math.max(viewport.clientWidth",
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("virtual table sizing is missing %q", expected)
		}
	}
}

func TestFilterPanelReservesSpaceAndKeepsLatestOutputVisible(t *testing.T) {
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	if !strings.Contains(css, ".plugin-action-panel {\n  position: static;") {
		t.Fatal("filter panel still overlays query output")
	}

	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	if !strings.Contains(javascript, "function keepLatestOutputVisible()") || strings.Count(javascript, "keepLatestOutputVisible();") < 2 {
		t.Fatal("opening and closing the filter panel must keep the latest output visible")
	}
}

func TestStaticAssetsRequireRevalidation(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("static response status=%d cache-control=%q", response.Code, response.Header().Get("Cache-Control"))
	}
}

func TestPageProvidesClearOutputsWithoutClearingHistory(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	if !strings.Contains(body, `id="clear-outputs"`) || !strings.Contains(body, `aria-label="Clear outputs"`) {
		t.Fatalf("page is missing the clear-output control: %s", body)
	}

	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		"clearOutputs: document.getElementById(\"clear-outputs\")",
		"commandHistory.splice(100)",
		"els.outputArea.replaceChildren()",
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("clear/history behavior is missing %q", expected)
		}
	}
}

func TestPageProvidesApprovedWorkbenchNavigationAndHistoryControls(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	for _, expected := range []string{
		`id="connection-rail"`,
		`id="connection-list"`,
		`id="toggle-left-panel"`,
		`id="toggle-right-panel"`,
		`id="history-search"`,
		`data-history-tool="all"`,
		`data-history-tool="aerospike"`,
		`data-history-tool="redis"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("approved workbench is missing %q", expected)
		}
	}
	if strings.Contains(body, `id="connection-tabs"`) {
		t.Fatal("page still renders redundant connection tabs")
	}
}

func TestPageUsesMinimalOrbyShell(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	for _, expected := range []string{
		`<title>Orby</title>`,
		`class="operator-main"`,
		`class="brand-copy"`,
		`query me maybe`,
		`top-connection-trigger`,
		`class="top-context"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("minimal canvas layout is missing %q", expected)
		}
	}
}

func TestOrbyHeaderAndConnectionSidebarUseApprovedCompactStyling(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	for _, expected := range []string{
		`href="/static/icons/orby.svg" type="image/svg+xml"`,
		`<img src="/static/icons/orby.svg" alt="">`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("approved Orby artwork is missing %q", expected)
		}
	}

	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{
		"grid-template-rows: 48px minmax(0, 1fr);",
		".brand-mark { width: 32px; height: 32px;",
		".panel-toggle { width: 32px; height: 32px;",
		".top-connection-trigger { width: auto;",
		".rail-toolbar { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr));",
		".rail-toolbar .icon-button { width: 100%; height: 30px; }",
		".rail-toolbar .action-icon { width: 13px; height: 13px; }",
		".connection-group { display: block; border: 1px solid var(--line);",
		".connection-item-copy strong { font-size: 12px;",
		".connection-item-copy small { margin-top: 3px; color: var(--muted); font: 11px",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("compact readable sidebar styling is missing %q", expected)
		}
	}
}

func TestPluginComposerRendersLeadingControlsOutsideTheQueryLine(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	if !strings.Contains(body, `id="plugin-leading"`) {
		t.Fatal("page is missing the plugin-defined leading control slot")
	}
	tool := strings.Index(body, `id="composer-tool-picker"`)
	leading := strings.Index(body, `id="plugin-leading"`)
	composer := strings.Index(body, `id="plugin-composer"`)
	if tool < 0 || leading < tool || composer < leading {
		t.Fatal("plugin leading controls must render after the plugin logo and before the query")
	}

	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		"pluginLeading: document.getElementById(\"plugin-leading\")",
		`element.placement === "leading"`,
		`element.hideNarrow`,
		`control.classList.add("composer-hide-narrow")`,
		"els.pluginLeading.replaceChildren",
		`els.queryForm.classList.toggle("has-leading"`,
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("leading plugin control rendering is missing %q", expected)
		}
	}
}

func TestLeadingComposerAndCompactHeaderStayAlignedAtBreakpoints(t *testing.T) {
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{
		".operator-composer.has-leading { grid-template-columns: 38px 34px minmax(260px, 1fr) 38px minmax(var(--run-btn-min), auto); }",
		".operator-composer.has-leading { grid-template-columns: 38px 34px minmax(300px, 1fr) 38px minmax(var(--run-btn-min), auto); }",
		".operator-composer.has-leading { grid-template-columns: 38px 34px minmax(0, 1fr) 38px minmax(var(--run-btn-min), auto); }",
		"top: 48px;",
		"inset: 48px 0 0;",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("compact responsive header/composer styling is missing %q", expected)
		}
	}
}

func TestConnectionListUsesOneOuterScrollbarWithoutClippingProfiles(t *testing.T) {
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	if !strings.Contains(css, ".connection-list { min-height: 0; overflow-y: auto; display: grid; grid-auto-rows: max-content;") {
		t.Fatal("connection profiles can still shrink and clip instead of overflowing the outer list")
	}
}

func TestEmptyStateDoesNotOfferExampleQuery(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(response.Body.String(), `id="use-example"`) {
		t.Fatal("empty state still renders the example query action")
	}
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(script), "useExample") || strings.Contains(string(script), "use-example") {
		t.Fatal("frontend still wires the removed example query action")
	}
}

func TestPluginComposerStaysAlignedAndHidesOptionalControlsWhenNarrow(t *testing.T) {
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{
		"height: 34px;",
		"flex-wrap: nowrap;",
		"overflow-x: auto;",
		"container-type: inline-size;",
		"@container (max-width: 760px)",
		".composer-hide-narrow { display: none !important; }",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("single-row responsive plugin composer styling is missing %q", expected)
		}
	}
}

func TestDesktopSidebarsDockResizeAndRememberLayout(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	for _, expected := range []string{
		`class="operator-main"`,
		`id="left-sidebar-resizer"`,
		`id="right-sidebar-resizer"`,
		`role="separator"`,
		`aria-orientation="vertical"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("resizable docked layout is missing %q", expected)
		}
	}

	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{
		"--left-sidebar-width: 280px;",
		"--right-sidebar-width: 340px;",
		"grid-template-columns: var(--left-sidebar-width) minmax(0, 1fr) var(--right-sidebar-width);",
		".operator-main.left-panel-collapsed {",
		".operator-main.right-panel-collapsed {",
		".sidebar-resizer {",
		"cursor: col-resize;",
		".sidebar-resizer-left { right: 0; }",
		".sidebar-resizer-right { left: 0; }",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("resizable docked CSS is missing %q", expected)
		}
	}

	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		`const workspaceLayoutKey = "orby_workspace_layout_v1"`,
		`function loadWorkspaceLayout()`,
		`function saveWorkspaceLayout()`,
		`function startSidebarResize(side, event)`,
		`leftSidebarResizer: document.getElementById("left-sidebar-resizer")`,
		`rightSidebarResizer: document.getElementById("right-sidebar-resizer")`,
		`addEventListener("pointerdown"`,
		`window.addEventListener("pointermove", move)`,
		`window.addEventListener("pointerup", stop, { once: true })`,
		`localStorage.setItem(workspaceLayoutKey`,
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("remembered sidebar behavior is missing %q", expected)
		}
	}
}

func TestRememberedSidebarLayoutIsAppliedBeforeFirstPaint(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	headEnd := strings.Index(body, "</head>")
	bootstrap := strings.Index(body, `localStorage.getItem("orby_workspace_layout_v1")`)
	if bootstrap < 0 || bootstrap > headEnd {
		t.Fatal("remembered sidebar state is not restored in the head before first paint")
	}
	for _, expected := range []string{
		`classList.toggle("left-panel-collapsed", layout.leftOpen === false)`,
		`classList.toggle("right-panel-collapsed", layout.rightOpen === false)`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("pre-paint sidebar bootstrap is missing %q", expected)
		}
	}

	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{
		"html.left-panel-collapsed .operator-main",
		"html.right-panel-collapsed .operator-main",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("pre-paint sidebar CSS is missing %q", expected)
		}
	}
}

func TestConnectionActionsShareOneHeaderToolbar(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	header := strings.Index(body, `class="rail-header"`)
	toolbar := strings.Index(body, `class="rail-toolbar"`)
	list := strings.Index(body, `id="connection-list"`)
	if header < 0 || toolbar < header || list < toolbar {
		t.Fatal("connection actions are not grouped directly below the Connections heading")
	}
	for _, expected := range []string{`id="new-connection"`, `id="save-connection"`, `id="duplicate-connection"`, `id="delete-connection"`} {
		if !strings.Contains(body[toolbar:list], expected) {
			t.Fatalf("connection toolbar is missing %q", expected)
		}
	}
	if strings.Contains(body, `class="rail-actions"`) {
		t.Fatal("connection actions still render in a separate bottom strip")
	}

	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{
		"grid-template-rows: 36px 38px minmax(0, 1fr);",
		".rail-toolbar { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr));",
		".rail-toolbar .icon-button { width: 100%; height: 30px; }",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("connection toolbar styling is missing %q", expected)
		}
	}
}

func TestResultCardsProvideCompactIconActions(t *testing.T) {
	server := mustServer(t)
	tool, _ := server.metadataFor("redis")
	response := httptest.NewRecorder()
	server.writeQueryResult(response, tool, queryRequest{}, queryResult{
		Tool: "redis", Query: "GET greeting", Format: "raw", Raw: "hello", IsRaw: true, Succeeded: true,
	}, "success")
	body := response.Body.String()
	for _, expected := range []string{
		`class="icon-btn cmd-toggle"`,
		`class="cmd-action-icon expand-icon"`,
		`class="icon-btn cmd-copy"`,
		`class="icon-btn cmd-close"`,
		`class="cmd-action-icon close-icon"`,
		`aria-label="Close output"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("result card is missing %q: %s", expected, body)
		}
	}
}

func TestFrontendWiresApprovedWorkbenchInteractions(t *testing.T) {
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	outputView, err := os.ReadFile("static/output_view.mjs")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script) + string(outputView)
	for _, expected := range []string{
		`connectionList: document.getElementById("connection-list")`,
		`toggleLeftPanel: document.getElementById("toggle-left-panel")`,
		`toggleRightPanel: document.getElementById("toggle-right-panel")`,
		`historySearch: document.getElementById("history-search")`,
		`function renderConnectionRail()`,
		`function togglePanel(side)`,
		`function filteredHistory()`,
		`function removeOutput(block)`,
		`const limit = expanded ? viewportHeight * .7 : 260`,
		`event.target.closest(".cmd-close")`,
		`commandHistory.splice(100)`,
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("approved workbench interaction is missing %q", expected)
		}
	}
	for _, removed := range []string{"connectionTabs", "renderConnectionTabs", "discardManualTabs"} {
		if strings.Contains(javascript, removed) {
			t.Fatalf("redundant connection-tab logic remains: %q", removed)
		}
	}
}

func TestWorkbenchPanelsDockWithoutCoveringCanvas(t *testing.T) {
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{
		".connection-rail {\n  position: relative;",
		".terminal-pane {\n  grid-column: 2;",
		".connection-sidebar {\n  position: relative;",
		".left-panel-collapsed .connection-rail",
		".right-panel-collapsed .connection-sidebar",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("collapsible sidebar styling is missing %q", expected)
		}
	}
}

func TestResponsivePanelsOpenAsAccessibleDrawers(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	for _, expected := range []string{`id="panel-backdrop"`, `aria-label="Close sidebar"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("responsive drawer markup is missing %q", expected)
		}
	}

	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{
		".operator-main.mobile-left-open .connection-rail",
		".operator-main.mobile-right-open .connection-sidebar",
		".operator-main.mobile-left-open .panel-backdrop",
		"position: fixed;",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("responsive drawer styling is missing %q", expected)
		}
	}

	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		`mobile-left-open`,
		`mobile-right-open`,
		`window.matchMedia("(max-width: 1050px)")`,
		`window.matchMedia("(max-width: 820px)")`,
		`function closeMobilePanels()`,
		`function closePanels()`,
		`els.panelBackdrop.addEventListener("click", closePanels)`,
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("responsive drawer behavior is missing %q", expected)
		}
	}
}

func TestConnectionActionsReflectFormValidity(t *testing.T) {
	server := mustServer(t)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	for _, expected := range []string{`id="connection-name" placeholder="Production cluster" required`, `id="host-input" placeholder="host or VM name" required`, `id="port-input" placeholder="3000" required`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("required connection field is missing %q", expected)
		}
	}

	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		"function updateConnectionActions()",
		"els.saveConnection.disabled = !valid",
		"els.duplicateConnection.disabled = !saved",
		"els.deleteConnection.disabled = !saved",
		"els.connect.disabled = !valid || pending",
		`"details-changed": "Unsaved changes"`,
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("connection action validation is missing %q", expected)
		}
	}
}

func TestComposerActionsStayVisibleAndNamed(t *testing.T) {
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	if !strings.Contains(javascript, `button.setAttribute("aria-label", actionLabel)`) || !strings.Contains(javascript, `const actionLabel = element.label || "Edit filters"`) {
		t.Fatal("plugin composer actions do not have an accessible name")
	}

	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{".composer-action-wrap {", "position: sticky;", "right: 0;", "z-index: 2;"} {
		if !strings.Contains(css, expected) {
			t.Fatalf("composer action visibility styling is missing %q", expected)
		}
	}
}

func TestControlsMeetAccessibleSizingAndContrast(t *testing.T) {
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{
		"--muted: #a1a1aa;",
		".panel-toggle { width: 32px; height: 32px;",
		".rail-toolbar .icon-button { width: 100%; height: 30px; }",
		".icon-btn { flex: 0 0 auto; width: 32px; height: 32px;",
		".history-filter { width: auto; min-height: 36px;",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("accessible control styling is missing %q", expected)
		}
	}
	if strings.Contains(css, ".connection-item-copy small { margin-top: 2px; color: #52525b") || strings.Contains(css, ".history-group { margin: 9px 6px 4px; color: #52525b") || strings.Contains(css, ".hist-copy small { margin-top: 2px; overflow: hidden; color: #52525b") {
		t.Fatal("secondary text still uses the low-contrast #52525b color")
	}
}

func TestSelectsUseShadcnStyleWithoutLosingNativeSemantics(t *testing.T) {
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{
		"background-image: url('/static/icons/chevron-down.svg');",
		"box-shadow: 0 1px 2px rgba(0, 0, 0, .35);",
		"select:hover:not(:disabled)",
		"select:focus-visible",
		"select:disabled",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("shadcn-style select treatment is missing %q", expected)
		}
	}
	if strings.Contains(css, "background-image: linear-gradient(45deg") {
		t.Fatal("selects still use the browser-like CSS triangle treatment")
	}
}

func TestFrontendGroupsPresetConnectionsWithoutAutoConnecting(t *testing.T) {
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		`import { connectionGroups, mergeConnections } from "./preconfigured_connections.mjs";`,
		`const presetConfig = window.ORBY_PRESET_CONFIG`,
		"function loadSavedConnections()",
		"return mergeConnections(loadSavedConnections(), presetConfig);",
		"connectionGroups(connections, presetConfig.profiles)",
		`els.connectionState.dataset.state !== "reachable"`,
		`connectionId: els.formConnectionID.value`,
		`? preferred : NEW_CONNECTION`,
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("preset connection integration is missing %q", expected)
		}
	}
}

func TestConnectionProfilesUseAccessiblePersistentDisclosures(t *testing.T) {
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		`import { loadCollapsedProfiles, setProfileCollapsed } from "./profile_state.mjs";`,
		`<details class="connection-group"`,
		`<summary class="connection-group-summary">`,
		`setProfileCollapsed(localStorage, details.dataset.profile, !details.open)`,
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("profile disclosure integration is missing %q", expected)
		}
	}

	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, expected := range []string{".connection-group-summary {", ".connection-group[open] .connection-group-chevron"} {
		if !strings.Contains(css, expected) {
			t.Fatalf("profile disclosure styling is missing %q", expected)
		}
	}
}

func TestFrontendUsesStableLeaseForConnectAndDisconnect(t *testing.T) {
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		"let manualConnectionID = newID()",
		"connection?.id || manualConnectionID",
		`tool: els.composerTool.value`,
		`connectionId: els.formConnectionID.value`,
		`fetch("/disconnect"`,
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("pooled connection UI is missing %q", expected)
		}
	}
}

func TestVirtualOutputRerunAndResizeWiring(t *testing.T) {
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	outputView, err := os.ReadFile("static/output_view.mjs")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script) + string(outputView)
	start := strings.Index(javascript, "async function rerunQuery")
	if start < 0 {
		t.Fatal("rerun function is missing")
	}
	end := strings.Index(javascript[start:], "async function handleToolChange")
	if end < 0 {
		t.Fatal("rerun function end is missing")
	}
	rerun := javascript[start : start+end]
	restore := strings.Index(rerun, "applyBlockConnection(block)")
	render := strings.Index(rerun, "await renderComposer(state)")
	format := strings.Index(rerun, "els.composerFormat.value = format")
	submit := strings.Index(rerun, "els.queryForm.requestSubmit()")
	if restore < 0 || render < restore || format < render || submit < format {
		t.Fatal("rerun must restore the plugin and connection before applying its saved format and submitting")
	}
	for _, expected := range []string{"new ResizeObserver", `window.addEventListener("resize"`, `setAttribute("role", "grid")`, `setAttribute("aria-rowcount"`} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("virtual output wiring is missing %q", expected)
		}
	}
}

func TestExpressionFilterUsesUserEnteredBinName(t *testing.T) {
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		`const bin = document.createElement("input")`,
		`bin.placeholder = "bin name"`,
		`bin.addEventListener("input"`,
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("editable bin input is missing %q", expected)
		}
	}
	if strings.Contains(javascript, "currentTool().composer.expression.binOptions") {
		t.Fatal("expression panel still requests inferred bin metadata")
	}
}

func TestSuccessfulOutputsUseCompactVirtualPayload(t *testing.T) {
	server := mustServer(t)
	tool, _ := server.metadataFor("redis")
	response := httptest.NewRecorder()
	server.writeQueryResult(response, tool, queryRequest{}, queryResult{
		Tool: "redis", Query: "KEYS *", Format: "table", Succeeded: true,
		Rows: []map[string]any{{"index": 0, "value": "first"}, {"index": 1, "value": "last"}},
	}, "success")
	body := response.Body.String()
	for _, expected := range []string{
		`class="virtual-output"`,
		`data-virtual-kind="table"`,
		`class="virtual-output-data"`,
		`"heads":["index","value"]`,
		`"rows":[["0","first"],["1","last"]]`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("virtual table payload is missing %q in %s", expected, body)
		}
	}
	if strings.Contains(body, `<tbody><tr>`) {
		t.Fatalf("server still renders every table row into the DOM: %s", body)
	}
}

func TestRawAndJSONOutputsUseVirtualTextPayloads(t *testing.T) {
	server := mustServer(t)
	tool, _ := server.metadataFor("redis")
	tests := []struct {
		name   string
		result queryResult
		kind   string
		text   string
	}{
		{name: "raw", kind: "raw", text: "first\\nlast", result: queryResult{Tool: "redis", Query: "KEYS *", Format: "raw", Raw: "first\nlast", IsRaw: true, Succeeded: true}},
		{name: "json", kind: "json", text: "first", result: queryResult{Tool: "redis", Query: "KEYS *", Format: "json", JSONValue: []any{"first", "last"}, HasJSONValue: true, Succeeded: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.writeQueryResult(response, tool, queryRequest{}, test.result, "success")
			body := response.Body.String()
			if !strings.Contains(body, `data-virtual-kind="`+test.kind+`"`) || !strings.Contains(body, `class="virtual-output-data"`) || !strings.Contains(body, test.text) {
				t.Fatalf("%s output is not a virtual text payload: %s", test.name, body)
			}
			if strings.Contains(body, `<pre class="raw-out">`) || strings.Contains(body, `<pre class="json-out">`) {
				t.Fatalf("%s output still renders the full text node", test.name)
			}
		})
	}
}

func TestVirtualPayloadCannotCloseItsScriptElement(t *testing.T) {
	server := mustServer(t)
	tool, _ := server.metadataFor("redis")
	response := httptest.NewRecorder()
	server.writeQueryResult(response, tool, queryRequest{}, queryResult{
		Tool: "redis", Query: "GET unsafe", Format: "raw", Raw: `</script><script>alert("unsafe")</script>`, IsRaw: true, Succeeded: true,
	}, "success")
	body := response.Body.String()
	if strings.Contains(body, `</script><script>alert`) || !strings.Contains(body, `\u003c/script\u003e\u003cscript\u003e`) {
		t.Fatalf("virtual payload is not safely embedded: %s", body)
	}
}

func TestDependentComposerSelectFitsItsPlaceholder(t *testing.T) {
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	expected := "if (dependency && !composerValues()[dependency]) {\n    select.innerHTML = `<option value=\"\">${escapeHTML(select.name)}…</option>`;\n    updateSelectTitle(select);\n    return;\n  }"
	javascript := string(script)
	if !strings.Contains(javascript, expected) {
		t.Fatal("dependent dropdown placeholder is not sized before returning")
	}
	if strings.Contains(javascript, "select.style.width") {
		t.Fatal("dropdown width is based on its content instead of sharing the available space")
	}
}

func TestServerRoutesAndMethods(t *testing.T) {
	server := mustServer(t)
	for _, path := range []string{"/query", "/connect", "/disconnect"} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s = %d", path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `id="query-form"`) || !strings.Contains(body, `"name":"aerospike"`) || !strings.Contains(body, `"text":"SELECT"`) || !strings.Contains(body, `id="plugin-composer"`) || !strings.Contains(body, `id="plugin-action-panel"`) || strings.Contains(body, `id="term-input"`) {
		t.Fatalf("index = %d %s", response.Code, response.Body.String())
	}
}

func mustServer(t *testing.T) *server {
	t.Helper()
	server, err := newServer()
	if err != nil {
		t.Fatal(err)
	}
	return server
}

type fakePlugin struct {
	request  queryRequest
	options  []pluginapi.Option
	connects int
	closed   int
}

type fakePluginConnection struct{ plugin *fakePlugin }

func (plugin *fakePlugin) Metadata() toolMetadata {
	return toolMetadata{Name: "fake", Label: "Fake", DefaultFormat: "json"}
}
func (plugin *fakePlugin) Run(request queryRequest) (queryResult, error) {
	plugin.request = request
	return queryResult{Tool: "fake", Query: "structured", Format: "json", State: request.Fields, Succeeded: true}, nil
}
func (plugin *fakePlugin) Connect(queryRequest) (pluginapi.Connection, error) {
	plugin.connects++
	return &fakePluginConnection{plugin: plugin}, nil
}
func (plugin *fakePlugin) Options(request queryRequest, resource string) ([]pluginapi.Option, error) {
	plugin.request = request
	if resource != "sets" {
		return nil, errors.New("bad resource")
	}
	return plugin.options, nil
}

func (connection *fakePluginConnection) Run(request queryRequest) (queryResult, error) {
	return connection.plugin.Run(request)
}

func (connection *fakePluginConnection) Options(request queryRequest, resource string) ([]pluginapi.Option, error) {
	return connection.plugin.Options(request, resource)
}

func (connection *fakePluginConnection) Close() error {
	connection.plugin.closed++
	return nil
}

func TestServerPersistsAndReusesConnectedPluginClient(t *testing.T) {
	server := mustServer(t)
	plugin := &fakePlugin{}
	server.plugins = map[string]pluginapi.Plugin{"fake": plugin}

	post := func(path string, values url.Values) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	copyValues := func(source url.Values) url.Values {
		copy := url.Values{}
		for key, values := range source {
			copy[key] = append([]string(nil), values...)
		}
		return copy
	}
	base := url.Values{"tool": {"fake"}, "host": {"database.internal"}, "port": {"3000"}}
	first := copyValues(base)
	first.Set("connectionId", "user-a")
	second := copyValues(base)
	second.Set("connectionId", "user-b")
	if response := post("/connect", first); response.Code != http.StatusOK {
		t.Fatalf("first connect = %d %s", response.Code, response.Body.String())
	}
	if response := post("/connect", second); response.Code != http.StatusOK {
		t.Fatalf("second connect = %d %s", response.Code, response.Body.String())
	}
	if plugin.connects != 1 {
		t.Fatalf("created %d plugin clients", plugin.connects)
	}
	query := copyValues(second)
	query.Set("namespace", "test")
	if response := post("/query", query); response.Code != http.StatusOK || plugin.request.Fields["namespace"] != "test" {
		t.Fatalf("query = %d %s request=%#v", response.Code, response.Body.String(), plugin.request)
	}
	if plugin.connects != 1 {
		t.Fatalf("query created another client: %d", plugin.connects)
	}
	if response := post("/disconnect", first); response.Code != http.StatusOK || plugin.closed != 0 {
		t.Fatalf("first disconnect = %d closed=%d", response.Code, plugin.closed)
	}
	if response := post("/disconnect", second); response.Code != http.StatusOK || plugin.closed != 1 {
		t.Fatalf("second disconnect = %d closed=%d", response.Code, plugin.closed)
	}
}

func TestConnectionStatusEndpointTracksPooledLeases(t *testing.T) {
	server := mustServer(t)
	defer server.connections.Close()
	plugin := &fakePlugin{}
	server.plugins = map[string]pluginapi.Plugin{"fake": plugin}
	values := url.Values{"tool": {"fake"}, "host": {"database.internal"}, "port": {"3000"}, "connectionId": {"saved-a"}, "leaseId": {"browser-a:saved-a"}}

	post := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	if response := post("/connect"); response.Code != http.StatusOK {
		t.Fatalf("connect = %d %s", response.Code, response.Body.String())
	}
	status := httptest.NewRecorder()
	server.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/connection-status?session=browser-a", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"active":["saved-a"]`) {
		t.Fatalf("status = %d %s", status.Code, status.Body.String())
	}
	if response := post("/disconnect"); response.Code != http.StatusOK {
		t.Fatalf("disconnect = %d %s", response.Code, response.Body.String())
	}
	status = httptest.NewRecorder()
	server.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/connection-status?session=browser-a", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"active":[]`) {
		t.Fatalf("status after disconnect = %d %s", status.Code, status.Body.String())
	}
}

func TestBrowserSessionsHaveIndependentPoolLeases(t *testing.T) {
	server := mustServer(t)
	defer server.connections.Close()
	plugin := &fakePlugin{}
	server.plugins = map[string]pluginapi.Plugin{"fake": plugin}
	post := func(path, lease string) *httptest.ResponseRecorder {
		values := url.Values{"tool": {"fake"}, "host": {"database.internal"}, "port": {"3000"}, "connectionId": {"preset:shared"}, "leaseId": {lease}}
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	if response := post("/connect", "browser-a:preset:shared"); response.Code != http.StatusOK {
		t.Fatalf("browser A connect = %d %s", response.Code, response.Body.String())
	}
	if response := post("/connect", "browser-b:preset:shared"); response.Code != http.StatusOK || plugin.connects != 1 {
		t.Fatalf("browser B connect = %d connects=%d %s", response.Code, plugin.connects, response.Body.String())
	}
	if response := post("/disconnect", "browser-a:preset:shared"); response.Code != http.StatusOK || plugin.closed != 0 {
		t.Fatalf("browser A disconnect = %d closed=%d %s", response.Code, plugin.closed, response.Body.String())
	}
	status := httptest.NewRecorder()
	server.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/connection-status?session=browser-b", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"active":["preset:shared"]`) {
		t.Fatalf("browser B status = %d %s", status.Code, status.Body.String())
	}
}

func TestFrontendMarksOnlyPooledConnectionsGreen(t *testing.T) {
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		"const activeConnectionIDs = new Set();",
		"const browserSessionID = loadBrowserSessionID();",
		`leaseId: els.formLeaseID.value`,
		`activeConnectionIDs.has(item.id) ? " is-connected" : ""`,
		"fetch(`/connection-status?session=${encodeURIComponent(browserSessionID)}`",
		"setInterval(refreshActiveConnections, 30_000)",
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("pool-backed active connection UI is missing %q", expected)
		}
	}
	stylesheet, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	if !strings.Contains(css, ".connection-item.is-connected .connection-dot { background: var(--green);") {
		t.Fatal("connected rail items do not have a green pool-state indicator")
	}
	if strings.Contains(css, ".connection-item.is-active .connection-dot { background: var(--green);") {
		t.Fatal("merely selecting a connection still makes its indicator green")
	}
}

func TestSelectingAnyListedConnectionImmediatelyConnects(t *testing.T) {
	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	javascript := string(script)
	for _, expected := range []string{
		"async function connectSelectedConnection()",
		"async function selectAndConnect(id, tool)",
		"if (!activeConnectionIDs.has(id)) await connectSelectedConnection();",
		"await selectAndConnect(item.dataset.connectionId, item.dataset.connectionTool)",
	} {
		if !strings.Contains(javascript, expected) {
			t.Fatalf("selection-triggered connection behavior is missing %q", expected)
		}
	}
}

// TestQueryCannotEscalatePresetEnvironmentViaFormOverride is the core safety
// property of the prod/stage switch: connections.json is the sole authority
// for a preset's environment, so a forged "environment=stage" form field on
// /query must not let a client unlock writes on a preset declared prod.
func TestQueryCannotEscalatePresetEnvironmentViaFormOverride(t *testing.T) {
	server := mustServer(t)
	plugin := &fakePlugin{}
	server.plugins = map[string]pluginapi.Plugin{"fake": plugin}
	server.presets = presetConfig{Profiles: []presetProfile{{ID: "local", Label: "Local", Connections: []presetConnection{
		{ID: "preset:prod-fake", Name: "prod-fake", Tool: "fake", Host: "database.internal", Port: "3000", Mode: "single", Environment: "prod", Fields: map[string]string{}},
	}}}}

	base := url.Values{"tool": {"fake"}, "host": {"database.internal"}, "port": {"3000"}, "connectionId": {"preset:prod-fake"}, "connectionName": {"prod-fake"}}
	if response := postForm(server, "/connect", base); response.Code != http.StatusOK {
		t.Fatalf("connect = %d %s", response.Code, response.Body.String())
	}

	query := url.Values{}
	for key, items := range base {
		query[key] = append([]string(nil), items...)
	}
	query.Set("query", "READ")
	query.Set("environment", "stage") // forged: connections.json declares this preset prod
	if response := postForm(server, "/query", query); response.Code != http.StatusOK {
		t.Fatalf("query = %d %s", response.Code, response.Body.String())
	}
	if plugin.request.Environment != "prod" {
		t.Fatalf("client-forged environment leaked through: plugin saw %q, want %q", plugin.request.Environment, "prod")
	}
}

// TestQueryTrustsAdHocConnectionEnvironment covers the other half: a non-preset
// connection has no server-side record to defer to, so the client's own
// (normalized) input is the only source of truth.
func TestQueryTrustsAdHocConnectionEnvironment(t *testing.T) {
	server := mustServer(t)
	plugin := &fakePlugin{}
	server.plugins = map[string]pluginapi.Plugin{"fake": plugin}

	values := url.Values{"tool": {"fake"}, "host": {"database.internal"}, "port": {"3000"}, "connectionId": {"manual-1"}, "query": {"READ"}, "environment": {"stage"}}
	if response := postForm(server, "/query", values); response.Code != http.StatusOK {
		t.Fatalf("query = %d %s", response.Code, response.Body.String())
	}
	if plugin.request.Environment != "stage" {
		t.Fatalf("ad-hoc environment = %q, want %q", plugin.request.Environment, "stage")
	}

	values.Set("environment", "not-a-real-environment")
	if response := postForm(server, "/query", values); response.Code != http.StatusOK {
		t.Fatalf("query = %d %s", response.Code, response.Body.String())
	}
	if plugin.request.Environment != "prod" {
		t.Fatalf("garbage environment should default to prod, got %q", plugin.request.Environment)
	}
}

func TestPresetRequestsRequireExplicitConnect(t *testing.T) {
	server := mustServer(t)
	plugin := &fakePlugin{options: []pluginapi.Option{{Value: "users", Label: "users"}}}
	server.plugins = map[string]pluginapi.Plugin{"fake": plugin}
	values := url.Values{
		"tool": {"fake"}, "host": {"database.internal"}, "port": {"3000"},
		"connectionId": {"preset:fake-k8-ci"}, "connectionName": {"fake-k8-ci"},
	}

	optionsRequest := httptest.NewRequest(http.MethodGet, "/plugin-options?"+values.Encode()+"&resource=sets&namespace=test", nil)
	optionsResponse := httptest.NewRecorder()
	server.ServeHTTP(optionsResponse, optionsRequest)
	if optionsResponse.Code != http.StatusConflict || plugin.connects != 0 {
		t.Fatalf("options=%d connects=%d body=%s", optionsResponse.Code, plugin.connects, optionsResponse.Body.String())
	}

	query := url.Values{}
	for key, items := range values {
		query[key] = append([]string(nil), items...)
	}
	query.Set("query", "READ")
	queryRequest := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(query.Encode()))
	queryRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	queryResponse := httptest.NewRecorder()
	server.ServeHTTP(queryResponse, queryRequest)
	if plugin.connects != 0 || !strings.Contains(queryResponse.Body.String(), "Connect this preset first") {
		t.Fatalf("connects=%d body=%s", plugin.connects, queryResponse.Body.String())
	}

	connectRequest := httptest.NewRequest(http.MethodPost, "/connect", strings.NewReader(values.Encode()))
	connectRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	connectResponse := httptest.NewRecorder()
	server.ServeHTTP(connectResponse, connectRequest)
	if connectResponse.Code != http.StatusOK || plugin.connects != 1 {
		t.Fatalf("connect=%d connects=%d body=%s", connectResponse.Code, plugin.connects, connectResponse.Body.String())
	}

	optionsResponse = httptest.NewRecorder()
	server.ServeHTTP(optionsResponse, httptest.NewRequest(http.MethodGet, "/plugin-options?"+values.Encode()+"&resource=sets&namespace=test", nil))
	if optionsResponse.Code != http.StatusOK || plugin.connects != 1 {
		t.Fatalf("connected options=%d connects=%d body=%s", optionsResponse.Code, plugin.connects, optionsResponse.Body.String())
	}
}

// blockingPlugin stalls Run until its query context is canceled, standing in
// for a slow database call during cancel-flow tests.
type blockingPlugin struct {
	started chan struct{}
}

func (plugin *blockingPlugin) Metadata() toolMetadata {
	return toolMetadata{Name: "blocking", Label: "Blocking", DefaultFormat: "raw"}
}

func (plugin *blockingPlugin) Connect(queryRequest) (pluginapi.Connection, error) {
	return &blockingPluginConnection{plugin: plugin}, nil
}

type blockingPluginConnection struct{ plugin *blockingPlugin }

func (connection *blockingPluginConnection) Run(request queryRequest) (queryResult, error) {
	if request.Context == nil {
		return queryResult{}, errors.New("query context is missing")
	}
	select {
	case <-connection.plugin.started:
	default:
		close(connection.plugin.started)
	}
	<-request.Context.Done()
	return queryResult{}, request.Context.Err()
}

func (connection *blockingPluginConnection) Close() error { return nil }

func postForm(server *server, path string, values url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

// Cancel = the UI aborting the query request; there is no /cancel endpoint.
func TestCancelAbortsRunningQuery(t *testing.T) {
	server := mustServer(t)
	defer server.connections.Close()
	plugin := &blockingPlugin{started: make(chan struct{})}
	server.plugins = map[string]pluginapi.Plugin{"blocking": plugin}
	values := url.Values{"tool": {"blocking"}, "host": {"db.internal"}, "port": {"3000"}, "query": {"SLOW"}}

	if response := postForm(server, "/connect", values); response.Code != http.StatusOK {
		t.Fatalf("connect = %d %s", response.Code, response.Body.String())
	}
	requestContext, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	request := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request = request.WithContext(requestContext)
	queryResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		queryResponse <- response
	}()
	select {
	case <-plugin.started:
	case <-time.After(5 * time.Second):
		t.Fatal("query never started")
	}

	cancelRequest()

	select {
	case response := <-queryResponse:
		body := response.Body.String()
		if response.Header().Get("X-Orby-Result") != "cancelled" || !strings.Contains(body, `data-result-status="cancelled"`) || !strings.Contains(body, "CANCELLED") {
			t.Fatalf("query response is not marked cancelled: %s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled query never returned")
	}
}

func TestPluginOptionsAndStructuredQuery(t *testing.T) {
	server := mustServer(t)
	plugin := &fakePlugin{options: []pluginapi.Option{{Value: "users", Label: "users"}}}
	server.plugins = map[string]pluginapi.Plugin{"fake": plugin}

	request := httptest.NewRequest(http.MethodGet, "/plugin-options?tool=fake&resource=sets&namespace=test&host=node&port=3000", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"options":[{"value":"users","label":"users"}]}`+"\n" || plugin.request.Fields["namespace"] != "test" {
		t.Fatalf("options = %d %s request=%#v", response.Code, response.Body.String(), plugin.request)
	}

	form := url.Values{"tool": {"fake"}, "host": {"node"}, "port": {"3000"}, "namespace": {"test"}, "set": {"users"}, "primaryKey": {""}, "expression": {`{"kind":"group"}`}}
	request = httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || plugin.request.Fields["set"] != "users" || !strings.Contains(response.Body.String(), `data-state=`) {
		t.Fatalf("query = %d %s request=%#v", response.Code, response.Body.String(), plugin.request)
	}
}

func TestUnknownPluginRendersErrorContract(t *testing.T) {
	server := mustServer(t)
	request := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader("tool=missing&query=LOOKUP"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Orby-Result") != "error" || !strings.Contains(response.Body.String(), "unknown plugin") {
		t.Fatalf("response = %d %#v %s", response.Code, response.Header(), response.Body.String())
	}
}

func TestRenderBlockPreservesHTMXAndEscaping(t *testing.T) {
	server := mustServer(t)
	tool, _ := server.metadataFor("aerospike")
	response := httptest.NewRecorder()
	server.writeQueryResult(response, tool, queryRequest{}, queryResult{Tool: "aerospike", Query: "SELECT * FROM t.s", Format: "json", Profile: "Local <app>", Rows: []map[string]any{{"z": []any{1, true}, "a": "<safe>"}}, State: map[string]string{"namespace": "t", "set": "s"}, Succeeded: true}, "success")
	body := response.Body.String()
	for _, expected := range []string{`hx-swap-oob="beforeend:#output-area"`, `data-result-status="success"`, `data-state=`, `Local &lt;app&gt;`, `class="virtual-output"`, `data-virtual-kind="json"`, `\u003csafe\u003e`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in %s", expected, body)
		}
	}
	if strings.Contains(body, `<details`) {
		t.Fatal("JSON output still uses the collapsible tree")
	}
	if strings.Index(body, `\"a\"`) >= strings.Index(body, `\"z\"`) {
		t.Fatal("JSON keys are not sorted")
	}
}

func TestRenderBlockUsesStructuredJSONValue(t *testing.T) {
	data := blockFor(toolMetadata{Name: "redis"}, queryRequest{}, queryResult{
		Tool: "redis", Query: "MGET first second", Format: "json",
		JSONValue: []any{"first", map[string]any{"name": "Ada"}}, HasJSONValue: true, Succeeded: true,
	}, "success")
	var output virtualOutput
	if err := json.Unmarshal([]byte(data.OutputJSON), &output); err != nil {
		t.Fatal(err)
	}
	if output.Kind != "json" || !strings.Contains(output.Text, "\n  \"first\",\n") || !strings.Contains(output.Text, "\n    \"name\": \"Ada\"\n") {
		t.Fatalf("data=%#v", data)
	}
}

func TestRenderBlockShowsResultCountOnlyWhenAvailable(t *testing.T) {
	counted := blockFor(toolMetadata{Name: "aerospike"}, queryRequest{}, queryResult{
		Tool: "aerospike", Query: "SELECT * FROM test.users", Format: "table",
		Rows: []map[string]any{{"name": "Ada"}, {"name": "Grace"}}, RowCount: 2, HasCount: true, Succeeded: true,
	}, "success")
	if counted.CountLabel != "2 records" {
		t.Fatalf("count label = %q", counted.CountLabel)
	}

	scalar := blockFor(toolMetadata{Name: "redis"}, queryRequest{}, queryResult{
		Tool: "redis", Query: "GET greeting", Format: "raw", Raw: "hello", IsRaw: true, Succeeded: true,
	}, "success")
	if scalar.CountLabel != "" {
		t.Fatalf("scalar count label = %q", scalar.CountLabel)
	}

	server := mustServer(t)
	response := httptest.NewRecorder()
	server.writeQueryResult(response, toolMetadata{Name: "aerospike"}, queryRequest{}, queryResult{
		Tool: "aerospike", Query: "SELECT * FROM test.users", Format: "table", RowCount: 0, HasCount: true, Succeeded: true,
	}, "success")
	if !strings.Contains(response.Body.String(), `class="cmd-count">0 records</span>`) {
		t.Fatalf("count is missing from output header: %s", response.Body.String())
	}
}

func TestTableUsesCompactJSONForNestedValues(t *testing.T) {
	data := blockFor(toolMetadata{Name: "redis"}, queryRequest{}, queryResult{
		Tool: "redis", Query: "GET campaign", Format: "table",
		Rows: []map[string]any{{
			"campaign_id":    "CMP471746",
			"placement_bids": []any{map[string]any{"placement": "SEARCH", "cpc": float64(205)}},
		}},
		Succeeded: true,
	}, "success")
	var output virtualOutput
	if err := json.Unmarshal([]byte(data.OutputJSON), &output); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output.Heads, []string{"campaign_id", "placement_bids"}) || !reflect.DeepEqual(output.Rows, [][]string{{"CMP471746", `[{"cpc":205,"placement":"SEARCH"}]`}}) {
		t.Fatalf("heads=%#v rows=%#v", output.Heads, output.Rows)
	}
}

func TestBlockCarriesConnectionIdentityForRerun(t *testing.T) {
	server := mustServer(t)
	tool, _ := server.metadataFor("redis")
	response := httptest.NewRecorder()
	server.writeQueryResult(response, tool, queryRequest{
		ConnectionID: "preset:redis-local", LeaseID: "sess:preset:redis-local",
		ConnectionName: "redis-local", Host: "127.0.0.1", Port: "6379", Mode: "single",
		Fields: map[string]string{"dbIndex": "2"},
	}, queryResult{
		Tool: "redis", Query: "GET greeting", Format: "raw", Raw: "hello", IsRaw: true, Succeeded: true,
	}, "success")
	body := response.Body.String()
	for _, expected := range []string{
		`data-connection-id="preset:redis-local"`,
		`data-lease-id="sess:preset:redis-local"`,
		`data-connection-name="redis-local"`,
		`data-host="127.0.0.1"`,
		`data-port="6379"`,
		`data-mode="single"`,
		`data-fields=` + `'` + `{&#34;dbIndex&#34;:&#34;2&#34;}` + `'`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("block is missing %q in %s", expected, body)
		}
	}
}

func TestResolvePort(t *testing.T) {
	tests := []struct {
		args []string
		env  string
		want int
		bad  bool
	}{
		{nil, "", 8080, false}, {nil, "9000", 9000, false}, {[]string{"6966"}, "9000", 6966, false},
		{[]string{"0"}, "", 0, true}, {[]string{"65536"}, "", 0, true}, {[]string{"bad"}, "", 0, true}, {[]string{"1", "2"}, "", 0, true},
	}
	for _, tt := range tests {
		got, err := resolvePort(tt.args, tt.env)
		if tt.bad != (err != nil) || !tt.bad && got != tt.want {
			t.Fatalf("resolvePort(%v, %q) = %d, %v", tt.args, tt.env, got, err)
		}
	}
}

func TestPluginFields(t *testing.T) {
	form := url.Values{
		"tool": {"aerospike"}, "namespace": {"audit"}, "set": {"events"},
	}
	if got := pluginFields(form); !reflect.DeepEqual(got, map[string]string{"namespace": "audit", "set": "events"}) {
		t.Fatalf("fields = %#v", got)
	}
}

func TestConnectionAddresses(t *testing.T) {
	got, err := pluginapi.ParseSeeds("one:3100, two, [::1]:3200", "3000", "connection")
	want := []pluginapi.Address{{Host: "one", Port: 3100}, {Host: "two", Port: 3000}, {Host: "::1", Port: 3200}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("addresses = %#v, %v", got, err)
	}
	if net.JoinHostPort("::1", "3000") != "[::1]:3000" {
		t.Fatal("IPv6 formatting changed")
	}
}

func TestConnectionKeyIncludesModeAndDBIndexForRedis(t *testing.T) {
	base := queryRequest{Host: "db.internal", Port: "6379"}
	single0, err := connectionKey("redis", queryRequest{Host: base.Host, Port: base.Port, Mode: "single", Fields: map[string]string{"dbIndex": "0"}})
	if err != nil {
		t.Fatal(err)
	}
	cluster0, err := connectionKey("redis", queryRequest{Host: base.Host, Port: base.Port, Mode: "cluster", Fields: map[string]string{"dbIndex": "0"}})
	if err != nil {
		t.Fatal(err)
	}
	single2, err := connectionKey("redis", queryRequest{Host: base.Host, Port: base.Port, Mode: "single", Fields: map[string]string{"dbIndex": "2"}})
	if err != nil {
		t.Fatal(err)
	}
	emptyMode, err := connectionKey("redis", queryRequest{Host: base.Host, Port: base.Port})
	if err != nil {
		t.Fatal(err)
	}
	wantSingle0 := "redis:db.internal:6379:mode=single:db=0"
	if single0 != wantSingle0 || cluster0 == single0 || single2 == single0 {
		t.Fatalf("redis keys must distinguish mode and dbIndex: single0=%q cluster0=%q single2=%q", single0, cluster0, single2)
	}
	if emptyMode != wantSingle0 {
		t.Fatalf("empty mode/dbIndex should normalize to single/0: %q, want %q", emptyMode, wantSingle0)
	}
	aerospike, err := connectionKey("aerospike", queryRequest{Host: base.Host, Port: base.Port, Mode: "cluster", Fields: map[string]string{"dbIndex": "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if aerospike != "aerospike:db.internal:6379" {
		t.Fatalf("non-redis tools should keep the plain host key: %q", aerospike)
	}
	if _, err := connectionKey("redis", queryRequest{Host: "bad", Port: "not-a-port"}); err == nil {
		t.Fatal("invalid port must fail")
	}
}
