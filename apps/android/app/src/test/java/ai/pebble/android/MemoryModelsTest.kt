package ai.pebble.android

import ai.pebble.android.data.HubJson
import ai.pebble.android.data.MemoryImportResp
import ai.pebble.android.data.MemoryIndexDoc
import ai.pebble.android.data.MemoryList
import ai.pebble.android.data.MemoryPatchResp
import ai.pebble.android.data.MemoryReindexResp
import ai.pebble.android.data.MemorySaveResp
import ai.pebble.android.data.MemoryStatusResp
import ai.pebble.android.data.SettingsData
import ai.pebble.android.ui.settings.MEMORY_KINDS
import ai.pebble.android.ui.settings.countMemoryLines
import ai.pebble.android.ui.settings.parseMemoryTags
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MemoryModelsTest {
    @Test
    fun decodesFullNativeMemoryListAndItems() {
        val raw = """
            {
              "backend": "pebble",
              "installed": true,
              "available": true,
              "magpie": {
                "installed": true,
                "hasLegacyItems": true,
                "legacyCount": 7,
                "injectedHarnesses": ["claude", "codex"]
              },
              "semantic": {
                "enabled": true,
                "modelReady": true,
                "indexReady": true,
                "active": true,
                "model": "text-embedding-3-small"
              },
              "counts": {
                "active": 12,
                "candidate": 3,
                "superseded": 1,
                "archived": 2
              },
              "items": [
                {
                  "id": "mem-1",
                  "path": "workspaces/_shared/memory/items/use-sqlite-mem1.md",
                  "scope": "global",
                  "kind": "decision",
                  "title": "Use SQLite FTS5",
                  "text": "Keep derived index in hub.db with unicode61 + trigram.",
                  "snippet": "Keep derived index...",
                  "why": "Zero CGO dependency",
                  "tags": ["arch", "sqlite"],
                  "status": "candidate",
                  "pin": true,
                  "confidence": 0.92,
                  "validFrom": "2026-11-27",
                  "sourceSession": "sess-9",
                  "sourceAuthor": "dream",
                  "origin": "memory",
                  "score": 1.42,
                  "createdAt": "2026-11-27T10:00:00Z",
                  "updatedAt": "2026-11-27T12:00:00Z"
                }
              ]
            }
        """.trimIndent()

        val list = HubJson.decodeFromString<MemoryList>(raw)
        assertEquals("pebble", list.backend)
        assertEquals(true, list.available)
        assertEquals(true, list.magpie?.installed)
        assertEquals(7, list.magpie?.legacyCount)
        assertEquals(listOf("claude", "codex"), list.magpie?.injectedHarnesses)
        assertEquals(12, list.counts?.active)
        assertEquals(3, list.counts?.candidate)
        assertEquals(1, list.counts?.superseded)
        assertEquals(2, list.counts?.archived)

        val item = list.items!!.single()
        assertEquals("mem-1", item.id)
        assertEquals("decision", item.kind)
        assertEquals("Use SQLite FTS5", item.title)
        assertEquals("Zero CGO dependency", item.why)
        assertEquals("candidate", item.status)
        assertEquals(true, item.pin)
        assertEquals(0.92, item.confidence ?: 0.0, 0.001)
        assertEquals("dream", item.sourceAuthor)
    }

    @Test
    fun decodesIndexStatusSettingsAndMutationResponses() {
        val idx = HubJson.decodeFromString<MemoryIndexDoc>(
            """{"ok":true,"path":"_shared/memory/MEMORY.md","content":"# Index\n- item 1","lines":2,"maxLines":100}""",
        )
        assertEquals(true, idx.ok)
        assertEquals(2, idx.lines)
        assertEquals(100, idx.maxLines)

        val status = HubJson.decodeFromString<MemoryStatusResp>(
            """
            {
              "backend": "pebble",
              "docCount": 14,
              "chunkCount": 42,
              "config": {
                "enabled": true,
                "coexistence": "isolated",
                "snapshotMaxChars": 8000,
                "disableClaudeAutoMemory": true,
                "dream": {"enabled": true, "dailyAt": "04:30", "model": "gpt-4o-mini", "minNewMessages": 5},
                "embedding": {"enabled": false, "baseUrl": "https://api.openai.com/v1", "hasApiKey": true, "model": "text-embedding-3-small"}
              },
              "recentDreams": [
                {"botId": "coder", "date": "2026-11-27", "path": "coder/memory/dreams/2026-11-27.md", "summary": "Consolidated 3 decisions"}
              ]
            }
            """.trimIndent(),
        )
        assertEquals(14, status.docCount)
        assertEquals(42, status.chunkCount)
        assertEquals("isolated", status.config?.coexistence)
        assertEquals("04:30", status.config?.dream?.dailyAt)
        assertEquals(true, status.config?.embedding?.hasApiKey)
        assertEquals("Consolidated 3 decisions", status.recentDreams?.single()?.summary)

        val settings = HubJson.decodeFromString<SettingsData>(
            """{"defaultHarness":"claude","memory":{"coexistence":"auto","dream":{"enabled":false,"dailyAt":"02:00"}}}""",
        )
        assertEquals("auto", settings.memory?.coexistence)
        assertEquals(false, settings.memory?.dream?.enabled)

        val saveObj = HubJson.decodeFromString<MemorySaveResp>(
            """{"ok":true,"id":"m1","similarExisting":{"id":"m0","text":"dup"}}""",
        )
        val saveArr = HubJson.decodeFromString<MemorySaveResp>(
            """{"ok":true,"id":"m2","similarExisting":[{"id":"m0","text":"dup"}]}""",
        )
        assertNotNull(saveObj.similarExisting)
        assertNotNull(saveArr.similarExisting)

        val patch = HubJson.decodeFromString<MemoryPatchResp>("""{"ok":true,"item":{"id":"m1","status":"active","pin":true}}""")
        assertEquals("active", patch.item?.status)
        assertEquals(true, patch.item?.pin)

        val reindex = HubJson.decodeFromString<MemoryReindexResp>("""{"ok":true,"docsIndexed":10,"chunksIndexed":30}""")
        assertEquals(10, reindex.docsIndexed)
        assertEquals(30, reindex.chunksIndexed)

        val imp = HubJson.decodeFromString<MemoryImportResp>("""{"ok":true,"imported":5,"skipped":2}""")
        assertEquals(5, imp.imported)
        assertEquals(2, imp.skipped)
    }

    @Test
    fun memoryHelpersCountLinesParseTagsAndIncludeDecisionKind() {
        assertEquals(0, countMemoryLines(""))
        assertEquals(0, countMemoryLines("\n\n"))
        assertEquals(1, countMemoryLines("hello"))
        assertEquals(3, countMemoryLines("a\nb\nc\n"))

        assertEquals(listOf("arch", "db", "ops"), parseMemoryTags(" arch, db , , ops "))
        assertTrue("decision" in MEMORY_KINDS)
        assertEquals(8, MEMORY_KINDS.size)

        val minimal = HubJson.decodeFromString<MemoryList>("""{"items":[]}""")
        assertNull(minimal.error)
        assertFalse(minimal.magpie?.installed == true)
    }
}
