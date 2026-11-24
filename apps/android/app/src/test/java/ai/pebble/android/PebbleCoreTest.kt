package ai.pebble.android

import ai.pebble.android.data.BotStatus
import ai.pebble.android.data.HubJson
import ai.pebble.android.data.countToolCalls
import ai.pebble.android.data.formatDurationMs
import ai.pebble.android.data.normalizeAddress
import ai.pebble.android.data.resolveDefaultModel
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
        assertEquals("claude-sonnet-4", resolveDefaultModel("magpie/claude-sonnet-4", "claude"))
        assertEquals("magpie/claude-sonnet-4", resolveDefaultModel("magpie/claude-sonnet-4", "opencode"))
        assertEquals(5, countToolCalls(listOf("bash×3", "read_file", "grep")))
        assertEquals("1m 5s", formatDurationMs(65_000.0))
        assertEquals("pebble-2", normalizeBotAvatar("cat"))
        assertEquals(fnv1a("assistant"), fnv1a("assistant"))
        assertEquals("#7aaa98", botColorHex("assistant", 3))

        val decoded = HubJson.decodeFromString<BotStatus>("""{"id":"a","name":"Test","unknownField":123}""")
        assertEquals("Test", decoded.displayName)
        assertFalse(decoded.isBusy)
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
}
