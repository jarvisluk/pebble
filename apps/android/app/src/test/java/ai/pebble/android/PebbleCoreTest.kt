package ai.pebble.android

import ai.pebble.android.data.BotStatus
import ai.pebble.android.data.BotTask
import ai.pebble.android.data.BotTaskEvent
import ai.pebble.android.data.BotsResp
import ai.pebble.android.data.CLIENT_PROTOCOL
import ai.pebble.android.data.HarnessInfo
import ai.pebble.android.data.HubJson
import ai.pebble.android.data.NodeInfo
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.PendingApproval
import ai.pebble.android.data.PendingQuestion
import ai.pebble.android.data.ServerInfo
import ai.pebble.android.data.StageKind
import ai.pebble.android.data.StageOpenEvent
import ai.pebble.android.data.StageRef
import ai.pebble.android.data.TakeoverRequest
import ai.pebble.android.data.countToolCalls
import ai.pebble.android.data.formatDurationMs
import ai.pebble.android.data.normalizeAddress
import ai.pebble.android.data.resolveDefaultModel
import ai.pebble.android.ui.desktop.BotStageState
import ai.pebble.android.ui.desktop.StageStateOps
import ai.pebble.android.ui.chat.MarkdownBlock
import ai.pebble.android.ui.chat.MarkdownParser
import ai.pebble.android.ui.chat.TableAlign
import ai.pebble.android.ui.chat.buildInlineMarkdown
import ai.pebble.android.ui.chat.renderMathAnnotated
import ai.pebble.android.ui.theme.PebblePalette
import ai.pebble.android.ui.theme.botColorHex
import ai.pebble.android.ui.theme.fnv1a
import ai.pebble.android.ui.theme.normalizeBotAvatar
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PebbleCoreTest {
    @Test
    fun normalizeAddressHandlesAllSchemesAndTokens() {
        val tc = normalizeAddress("tailcat://connect/tc123?port=3789#token=secret%2B1")
        assertEquals("tailcat://connect/tc123?port=3789", tc.cleanAddress)
        assertEquals("secret+1", tc.token)
        assertTrue(tc.isTailcat)
        assertFalse(tc.isTailscale)

        val ts = normalizeAddress("tailscale://mac-mini.ts.net:3789#token=abc")
        assertEquals("tailscale://mac-mini.ts.net:3789", ts.cleanAddress)
        assertEquals("abc", ts.token)
        assertTrue(ts.isTailscale)

        val lan = normalizeAddress("192.168.1.10:3789/")
        assertEquals("http://192.168.1.10:3789", lan.cleanAddress)
        assertEquals("", lan.token)
        assertFalse(lan.isTailcat)
    }

    @Test
    fun markdownParserSplitsCodeHeadingsListsAndQuotes() {
        val blocks = MarkdownParser.parse(
            """
            # Title
            > Quote line

            - item one
            - item two

            ```kotlin
            println("hi")
            ```
            """.trimIndent(),
        )
        assertEquals(4, blocks.size)
        assertTrue(blocks[0] is MarkdownBlock.Heading)
        assertTrue(blocks[1] is MarkdownBlock.Quote)
        assertTrue(blocks[2] is MarkdownBlock.ListBlock)
        val code = blocks[3] as MarkdownBlock.Code
        assertEquals("kotlin", code.lang)
        assertEquals("println(\"hi\")", code.body)
    }

    @Test
    fun storeHelpersAndLenientDecodingWork() {
        assertEquals("claude-sonnet-4", resolveDefaultModel("claude-sonnet-4", "claude"))
        assertEquals("openai/gpt-5", resolveDefaultModel("openai/gpt-5", "opencode"))
        assertEquals("", resolveDefaultModel("openai/gpt-5", "claude"))
        assertEquals(5, countToolCalls(listOf("bash×3", "read_file", "grep")))
        assertEquals("1m 5s", formatDurationMs(65_000.0))
        assertEquals("pebble-2", normalizeBotAvatar("cat"))
        assertEquals(fnv1a("assistant"), fnv1a("assistant"))
        assertEquals("#7aaa98", botColorHex("assistant", 3))

        val decoded = HubJson.decodeFromString<BotStatus>("""{"id":"a","name":"Test","unknownField":123}""")
        assertEquals("Test", decoded.displayName)
        assertFalse(decoded.isBusy)

        val harness = HubJson.decodeFromString<HarnessInfo>(
            """{"id":"codex","label":"Codex","installed":true,"magpie":{"installed":true,"connected":false,"nativeModel":"codex/gpt-6-luna"}}""",
        )
        assertEquals(true, harness.installed)
    }

    @Test
    fun markdownSupportsGfmTablesTasksLatexDetailsAndSanitization() {
        val blocks = MarkdownParser.parse(
            """
            | Name | Score | Trend |
            | :--- | :---: | ---: |
            | Alpha | 95 | +5 |

            - [x] Done task
            - [ ] Pending task

            $$
            E = mc^2
            $$

            \[
            \frac{a + b}{c}
            \]

            <details open>
            <summary>Logs</summary>
            Hidden **detail**
            </details>

            <script>alert(1)</script>
            """.trimIndent(),
        )
        val table = blocks.filterIsInstance<MarkdownBlock.Table>().single()
        assertEquals(listOf("Name", "Score", "Trend"), table.headers)
        assertEquals(listOf(TableAlign.LEFT, TableAlign.CENTER, TableAlign.RIGHT), table.aligns)
        assertEquals(listOf("Alpha", "95", "+5"), table.rows.single())

        val list = blocks.filterIsInstance<MarkdownBlock.ListBlock>().single()
        assertEquals(2, list.items.size)
        assertTrue(list.items[0].task && list.items[0].checked)
        assertTrue(list.items[1].task && !list.items[1].checked)

        val maths = blocks.filterIsInstance<MarkdownBlock.Math>()
        assertEquals(2, maths.size)
        assertEquals("E = mc^2", maths[0].expr)
        assertEquals("\\frac{a + b}{c}", maths[1].expr)

        val details = blocks.filterIsInstance<MarkdownBlock.Details>().single()
        assertEquals("Logs", details.summary)
        assertTrue(details.isOpen)
        assertEquals(1, details.body.size)

        assertFalse(blocks.any { it.toString().contains("alert(1)") })

        val palette = PebblePalette.of(false)
        val inlineEscaped = buildInlineMarkdown("Price is \\\$100 and math is \$\\alpha + \\beta\$ and \\(\\gamma\\)", palette, palette.text)
        assertTrue(inlineEscaped.text.contains("$100"))
        assertTrue(inlineEscaped.text.contains("α + β"))
        assertTrue(inlineEscaped.text.contains("γ"))

        val unsafeInline = buildInlineMarkdown("<script>bad()</script><b>Safe</b>", palette, palette.text)
        assertFalse(unsafeInline.text.contains("bad()"))
        assertTrue(unsafeInline.text.contains("Safe"))

        val mathRendered = renderMathAnnotated("\\frac{\\alpha}{\\beta} + x^2", palette.text)
        assertTrue(mathRendered.text.contains("α/β"))
    }

    @Test
    fun protocolCompatibilityModelsAndTargetHelpersWork() {
        val modern = HubJson.decodeFromString<BotsResp>(
            """
            {
              "bots": [{"id": "b1", "name": "Alpha"}],
              "server": {
                "protocolVersion": 2,
                "minClientProtocol": 1,
                "minNodeProtocol": 1,
                "version": "0.1.0",
                "capabilities": ["desktop.ws_h264", "stage.files"],
                "futureField": {"nested": true}
              }
            }
            """.trimIndent(),
        )
        assertEquals(2, modern.server?.protocolVersion)
        assertEquals(listOf("desktop.ws_h264", "stage.files"), modern.server?.capabilities)

        val legacy = HubJson.decodeFromString<BotsResp>("""{"bots":[{"id":"b1"}]}""")
        assertEquals(null, legacy.server)
        assertEquals(1, legacy.bots?.size)

        val unknownServer = HubJson.decodeFromString<ServerInfo>("""{"protocolVersion":3,"brandNewFlag":true}""")
        assertEquals(3, unknownServer.protocolVersion)

        assertTrue(NodeInfo(id = "n1", protocolVersion = 1).isOutdated)
        assertTrue(NodeInfo(id = "n2", protocolVersion = null).isOutdated)
        assertFalse(NodeInfo(id = "n3", protocolVersion = CLIENT_PROTOCOL).isOutdated)

        assertTrue(PebbleStore.isLocalDesktopTarget(""))
        assertTrue(PebbleStore.isLocalDesktopTarget("local"))
        assertTrue(PebbleStore.isLocalDesktopTarget("virtual:bot1"))
        assertTrue(PebbleStore.isLocalDesktopTarget("local:virtual:bot1"))
        assertFalse(PebbleStore.isLocalDesktopTarget("node-1"))
        assertFalse(PebbleStore.isLocalDesktopTarget("node-1:virtual:bot1"))
    }

    @Test
    fun taskAndNodeCapabilityModelsDecodeCleanly() {
        val task = HubJson.decodeFromString<BotTask>(
            """
            {
              "id": "tsk_1",
              "botId": "b1",
              "kind": "worker",
              "title": "Run tests",
              "prompt": "go test ./...",
              "status": "waiting_approval",
              "deviceId": "mac-mini",
              "harness": "claude",
              "durationMs": 1250
            }
            """.trimIndent(),
        )
        assertEquals("Run tests", task.displayTitle)
        assertTrue(task.isActive)
        assertTrue(task.isWaiting)
        assertFalse(task.isTerminal)

        val ev = HubJson.decodeFromString<BotTaskEvent>(
            """{"seq": 7, "taskId": "tsk_1", "botId": "b1", "eventType": "tool_use", "payload": {"tool": "bash"}}""",
        )
        assertEquals(7L, ev.seq)
        assertEquals("tool_use", ev.eventType)

        val node = HubJson.decodeFromString<NodeInfo>(
            """
            {
              "id": "n1",
              "name": "Mac Mini",
              "permissions": ["files", "exec", "desktop", "agent"],
              "maxPermissionMode": "edit",
              "activeTaskIds": ["tsk_1"],
              "harnesses": [{"kind": "claude", "label": "Claude Code", "installed": true, "authStatus": "ready"}],
              "workspaces": [{"id": "ws1", "name": "app", "path": "/Users/dev/app", "isGit": true, "defaultBranch": "main"}]
            }
            """.trimIndent(),
        )
        assertEquals("edit", node.maxPermissionMode)
        assertEquals(1, node.harnesses?.size)
        assertEquals("Claude Code", node.harnesses?.first()?.title)
        assertEquals(1, node.workspaces?.size)
        assertEquals(listOf("tsk_1"), node.activeTaskIds)

        val apr = HubJson.decodeFromString<PendingApproval>(
            """{"id": "apr_1", "botId": "b1", "taskId": "tsk_1", "deviceId": "mac-mini", "toolName": "bash"}""",
        )
        assertEquals("tsk_1", apr.taskId)
        assertEquals("mac-mini", apr.deviceId)

        val q = HubJson.decodeFromString<PendingQuestion>(
            """{"id": "q_1", "botId": "b1", "taskId": "tsk_1", "deviceId": "mac-mini", "fields": []}""",
        )
        assertEquals("tsk_1", q.taskId)

        val to = HubJson.decodeFromString<TakeoverRequest>(
            """{"id": "to_1", "botId": "b1", "taskId": "tsk_1", "deviceId": "mac-mini", "reason": "Sign in"}""",
        )
        assertEquals("tsk_1", to.taskId)
        assertEquals("mac-mini", to.deviceId)
    }

    @Test
    fun stageStateOpsAndRefDecodingWork() {
        val bot = HubJson.decodeFromString<BotStatus>(
            """{"id":"b1","latestStage":{"id":"file:notes/plan.md","kind":"file","path":"notes/plan.md","title":"Plan"}}""",
        )
        assertEquals(StageKind.FILE, bot.latestStage?.resolvedKind)
        assertEquals("Plan", bot.latestStage?.title)

        val ev = StageOpenEvent(id = "file:README.md", kind = "file", path = "README.md", autoOpen = false)
        val fileTab = StageStateOps.tabFromOpenEvent(ev, filesTitle = "Files", desktopTitle = "Desktop", newTabTitle = "New Tab")
        assertEquals("README.md", fileTab.title)

        var st = BotStageState()
        st = StageStateOps.ensureNonEmpty(st, newTabTitle = "New Tab")
        assertEquals(1, st.tabs.size)
        assertEquals(StageKind.NEW, st.activeTab?.kind)

        val desk = StageStateOps.desktopTab(target = "local", title = "Desktop")
        st = StageStateOps.replaceNewOrOpen(st, desk)
        assertEquals(1, st.tabs.size)
        assertEquals("desktop", st.activeTabId)

        st = StageStateOps.upsert(st, fileTab, activate = true)
        assertEquals(2, st.tabs.size)
        assertEquals("file:README.md", st.activeTabId)

        st = StageStateOps.close(st, "file:README.md", newTabTitle = "New Tab")
        assertEquals(1, st.tabs.size)
        assertEquals("desktop", st.activeTabId)

        st = StageStateOps.close(st, "desktop", newTabTitle = "New Tab")
        assertEquals(1, st.tabs.size)
        assertEquals(StageKind.NEW, st.activeTab?.kind)
    }
}
