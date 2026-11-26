package ai.pebble.android.ui.desktop

import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.FileProvider
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.WorkspaceFileContent
import ai.pebble.android.data.WorkspaceFileEntry
import ai.pebble.android.data.WorkspaceListResult
import ai.pebble.android.data.errText
import ai.pebble.android.ui.chat.MarkdownText
import ai.pebble.android.ui.theme.BotAccent
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.TagPill
import coil3.compose.SubcomposeAsyncImage
import kotlinx.coroutines.launch
import java.util.Locale
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons

@Composable
fun WorkspaceFilesView(
    store: PebbleStore,
    botId: String,
    showHidden: Boolean,
    refreshTrigger: Int,
    onOpenFile: (String) -> Unit,
    modifier: Modifier = Modifier,
    inWideStage: Boolean = false,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()
    val botIdx = store.bots.indexOfFirst { it.id == botId }.coerceAtLeast(0)
    val botObj = store.bots.getOrNull(botIdx)
    val botPal = p.bot(botId, botIdx, botObj?.color)

    var dirPath by remember(botId) { mutableStateOf("") }
    var listResult by remember(botId) { mutableStateOf<WorkspaceListResult?>(null) }
    var listLoading by remember(botId) { mutableStateOf(false) }
    var listErr by remember(botId) { mutableStateOf("") }
    var listSeq by remember(botId) { mutableIntStateOf(0) }

    suspend fun loadDir(path: String) {
        if (botId.isEmpty()) return
        listSeq += 1
        val seq = listSeq
        listLoading = true
        listErr = ""
        try {
            val res = store.listWorkspaceFiles(botId, path, showHidden)
            if (seq != listSeq) return
            listResult = res
            dirPath = res.path
        } catch (e: Throwable) {
            if (seq != listSeq) return
            listErr = errText(e, store.t("opFailed"))
        }
        if (seq == listSeq) listLoading = false
    }

    LaunchedEffect(botId) {
        dirPath = ""
        listResult = null
        loadDir("")
    }

    LaunchedEffect(showHidden, refreshTrigger) {
        if (listResult != null || listErr.isNotEmpty()) {
            loadDir(dirPath)
        }
    }

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(p.page),
    ) {
        if (inWideStage) {
            Surface(
                color = p.card,
                shape = RoundedCornerShape(20.dp),
                modifier = Modifier
                    .fillMaxSize()
                    .padding(12.dp),
            ) {
                WorkspaceListPanel(
                    store = store,
                    botPal = botPal,
                    dirPath = dirPath,
                    listResult = listResult,
                    listLoading = listLoading,
                    listErr = listErr,
                    selectedPath = null,
                    isSplit = false,
                    inCard = true,
                    onLoadDir = { scope.launch { loadDir(it) } },
                    onOpenFile = onOpenFile,
                )
            }
        } else {
            WorkspaceListPanel(
                store = store,
                botPal = botPal,
                dirPath = dirPath,
                listResult = listResult,
                listLoading = listLoading,
                listErr = listErr,
                selectedPath = null,
                isSplit = false,
                onLoadDir = { scope.launch { loadDir(it) } },
                onOpenFile = onOpenFile,
            )
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun WorkspaceListPanel(
    store: PebbleStore,
    botPal: BotAccent,
    dirPath: String,
    listResult: WorkspaceListResult?,
    listLoading: Boolean,
    listErr: String,
    selectedPath: String?,
    isSplit: Boolean,
    onLoadDir: (String) -> Unit,
    onOpenFile: (String) -> Unit,
    inCard: Boolean = false,
) {
    val p = LocalPalette.current
    val segments = remember(dirPath) {
        if (dirPath.isEmpty()) emptyList() else dirPath.split('/').filter { it.isNotEmpty() }
    }

    Column(modifier = Modifier.fillMaxSize()) {
        if (segments.isNotEmpty()) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Row(
                    modifier = Modifier
                        .clip(RoundedCornerShape(6.dp))
                        .clickable { onLoadDir("") }
                        .padding(horizontal = 4.dp, vertical = 2.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Icon(painterResource(PebbleIcons.Folder), contentDescription = null, tint = p.text2, modifier = Modifier.size(15.dp))
                    Text(store.t("rootWorkspace"), color = p.text2, fontSize = PebbleFont.secondary, fontWeight = FontWeight.Medium)
                }
                segments.forEachIndexed { idx, seg ->
                    Icon(
                        painter = painterResource( PebbleIcons.KeyboardArrowRight),
                        contentDescription = null,
                        tint = p.mute,
                        modifier = Modifier.size(14.dp),
                    )
                    val target = segments.take(idx + 1).joinToString("/")
                    val isLast = idx == segments.lastIndex
                    Text(
                        text = seg,
                        color = if (isLast) p.text else p.text2,
                        fontSize = PebbleFont.secondary,
                        fontWeight = if (isLast) FontWeight.SemiBold else FontWeight.Medium,
                        modifier = Modifier
                            .clip(RoundedCornerShape(6.dp))
                            .clickable { onLoadDir(target) }
                            .padding(horizontal = 4.dp, vertical = 2.dp),
                    )
                }
            }
            HorizontalDivider(thickness = 0.5.dp, color = p.s2)
        }

        PullToRefreshBox(
            isRefreshing = listLoading && listResult != null,
            onRefresh = { onLoadDir(dirPath) },
            modifier = Modifier.weight(1f),
        ) {
            when {
                listLoading && listResult == null -> {
                    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator(color = p.accent)
                    }
                }
                listErr.isNotEmpty() && listResult == null -> {
                    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        Column(
                            horizontalAlignment = Alignment.CenterHorizontally,
                            verticalArrangement = Arrangement.spacedBy(10.dp),
                            modifier = Modifier.padding(24.dp),
                        ) {
                            Icon(painterResource(PebbleIcons.Warning), contentDescription = null, tint = p.danger, modifier = Modifier.size(32.dp))
                            Text(listErr, color = p.text2, fontSize = PebbleFont.secondary, textAlign = TextAlign.Center)
                            Button(
                                onClick = { onLoadDir(dirPath) },
                                colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                            ) {
                                Text(store.t("retry"))
                            }
                        }
                    }
                }
                else -> {
                    val entries = listResult?.entries.orEmpty()
                    if (entries.isEmpty() && dirPath.isEmpty()) {
                        Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                            Column(
                                horizontalAlignment = Alignment.CenterHorizontally,
                                verticalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                Icon(painterResource(PebbleIcons.Folder), contentDescription = null, tint = p.mute, modifier = Modifier.size(36.dp))
                                Text(store.t("emptyFolder"), color = p.text2, fontSize = PebbleFont.body)
                            }
                        }
                    } else {
                        LazyColumn(
                            modifier = Modifier.fillMaxSize(),
                            contentPadding = PaddingValues(if (isSplit || inCard) 8.dp else 12.dp),
                        ) {
                            item(key = "card") {
                                Surface(
                                    color = if (isSplit || inCard) Color.Transparent else p.card,
                                    shape = if (isSplit || inCard) RoundedCornerShape(0.dp) else PebbleDimens.cardShape,
                                    modifier = Modifier.fillMaxWidth(),
                                ) {
                                    Column(modifier = Modifier.fillMaxWidth()) {
                                        if (dirPath.isNotEmpty()) {
                                            Row(
                                                modifier = Modifier
                                                    .fillMaxWidth()
                                                    .clickable { onLoadDir(listResult?.parent.orEmpty()) }
                                                    .padding(horizontal = 14.dp, vertical = 11.dp),
                                                verticalAlignment = Alignment.CenterVertically,
                                                horizontalArrangement = Arrangement.spacedBy(12.dp),
                                            ) {
                                                Icon(painterResource(PebbleIcons.Reply), contentDescription = null, tint = p.text2, modifier = Modifier.size(18.dp))
                                                Text("..", color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
                                            }
                                            HorizontalDivider(
                                                modifier = Modifier.padding(horizontal = 14.dp),
                                                thickness = 0.5.dp,
                                                color = p.s2,
                                            )
                                        }
                                        if (entries.isEmpty()) {
                                            Text(
                                                text = store.t("emptyFolder"),
                                                color = p.mute,
                                                fontSize = PebbleFont.body,
                                                textAlign = TextAlign.Center,
                                                modifier = Modifier
                                                    .fillMaxWidth()
                                                    .padding(vertical = 24.dp),
                                            )
                                        } else {
                                            entries.forEachIndexed { idx, ent ->
                                                val isSel = isSplit && !ent.isDir && selectedPath == ent.path
                                                val prevSel = idx > 0 && isSplit && !entries[idx - 1].isDir && selectedPath == entries[idx - 1].path
                                                if (idx > 0 && !isSel && !prevSel) {
                                                    HorizontalDivider(
                                                        modifier = Modifier.padding(horizontal = 14.dp),
                                                        thickness = 0.5.dp,
                                                        color = p.s2,
                                                    )
                                                }
                                                WorkspaceEntryRow(
                                                    store = store,
                                                    botPal = botPal,
                                                    entry = ent,
                                                    selected = isSel,
                                                    isSplit = isSplit,
                                                    onClick = {
                                                        if (ent.isDir) onLoadDir(ent.path) else onOpenFile(ent.path)
                                                    },
                                                )
                                            }
                                        }
                                    }
                                }
                            }
                            if (listResult?.truncated == true) {
                                item(key = "trunc") {
                                    Text(
                                        text = store.t("filesTruncatedHint"),
                                        color = p.mute,
                                        fontSize = PebbleFont.caption,
                                        modifier = Modifier.padding(horizontal = 6.dp, vertical = 8.dp),
                                    )
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun WorkspaceEntryRow(
    store: PebbleStore,
    botPal: BotAccent,
    entry: WorkspaceFileEntry,
    selected: Boolean,
    isSplit: Boolean,
    onClick: () -> Unit,
) {
    val p = LocalPalette.current
    val ext = entry.name.substringAfterLast('.', "").lowercase()
    val icon = when {
        entry.isDir && entry.isShared == true -> PebbleIcons.FolderShared
        entry.isDir -> PebbleIcons.Folder
        ext in setOf("png", "jpg", "jpeg", "gif", "webp") -> PebbleIcons.Image
        ext in setOf("go", "kt", "kts", "swift", "ts", "tsx", "js", "jsx", "py", "rs", "json", "yaml", "yml", "toml", "sh", "sql", "css", "html") -> PebbleIcons.Code
        else -> PebbleIcons.Description
    }
    val iconColor = when {
        entry.isDir && entry.isShared == true -> botPal.collabInk
        entry.isDir -> p.accent
        else -> p.text2
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = if (isSplit) 4.dp else 0.dp)
            .clip(RoundedCornerShape(if (isSplit) 12.dp else 0.dp))
            .background(if (selected) p.sel else Color.Transparent)
            .clickable(onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(painterResource(icon), contentDescription = null, tint = iconColor, modifier = Modifier.size(20.dp))
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                text = entry.name,
                color = p.text,
                fontSize = PebbleFont.cardTitle,
                fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Medium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (entry.isShared == true || !entry.isDir) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    if (entry.isShared == true) {
                        TagPill(store.t("sharedBadge"), botPal.collabBg, botPal.collabInk)
                    }
                    if (!entry.isDir) {
                        Text(formatByteSize(entry.size), color = p.text2, fontSize = PebbleFont.secondary)
                    }
                }
            }
        }
        if (entry.isDir) {
            Icon(
                painter = painterResource( PebbleIcons.KeyboardArrowRight),
                contentDescription = null,
                tint = p.mute,
                modifier = Modifier.size(18.dp),
            )
        }
    }
}

@Composable
internal fun WorkspacePreviewPanel(
    store: PebbleStore,
    path: String,
    preview: WorkspaceFileContent?,
    imageUrl: String?,
    loading: Boolean,
    error: String,
    downloading: Boolean,
    toastMsg: String,
    isSplit: Boolean,
    onBack: () -> Unit,
    onRetry: () -> Unit,
    onQuotePath: () -> Unit,
    onShare: () -> Unit,
    inCard: Boolean = false,
) {
    val p = LocalPalette.current

    Column(modifier = Modifier.fillMaxSize()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .background(if (isSplit || inCard) Color.Transparent else p.card)
                .padding(horizontal = 12.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            if (!isSplit) {
                IconButton(onClick = onBack, modifier = Modifier.size(34.dp)) {
                    Icon(painterResource(PebbleIcons.ArrowBack), contentDescription = store.t("back"), tint = p.accent)
                }
            }
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(1.dp)) {
                Text(
                    text = path.substringAfterLast('/'),
                    color = p.text,
                    fontSize = PebbleFont.cardTitle,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                if (path.contains('/')) {
                    Text(
                        text = path,
                        color = p.text2,
                        fontSize = PebbleFont.caption,
                        fontFamily = MonoFont,
                        maxLines = 1,
                        overflow = TextOverflow.MiddleEllipsis,
                    )
                }
            }
            OutlinedButton(
                onClick = onQuotePath,
                contentPadding = PaddingValues(horizontal = 10.dp, vertical = 6.dp),
            ) {
                Icon(painterResource(PebbleIcons.FormatQuote), contentDescription = store.t("insertToChat"), tint = p.accent, modifier = Modifier.size(16.dp))
                if (isSplit) {
                    Spacer(Modifier.width(4.dp))
                    Text(store.t("insertToChat"), color = p.text, fontSize = PebbleFont.secondary, maxLines = 1)
                }
            }
            if (downloading) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.accent)
            } else {
                IconButton(onClick = onShare, modifier = Modifier.size(34.dp)) {
                    Icon(painterResource(PebbleIcons.IosShare), contentDescription = store.t("openQuickLook"), tint = p.accent, modifier = Modifier.size(18.dp))
                }
            }
        }
        HorizontalDivider(thickness = 0.5.dp, color = p.s2)

        if (toastMsg.isNotEmpty()) {
            Text(
                text = toastMsg,
                color = p.text2,
                fontSize = PebbleFont.secondary,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
            )
        }

        when {
            loading -> {
                Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(color = p.accent)
                }
            }
            error.isNotEmpty() -> {
                Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    Column(
                        horizontalAlignment = Alignment.CenterHorizontally,
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                        modifier = Modifier.padding(24.dp),
                    ) {
                        Icon(painterResource(PebbleIcons.Warning), contentDescription = null, tint = p.danger, modifier = Modifier.size(32.dp))
                        Text(error, color = p.text2, fontSize = PebbleFont.secondary, textAlign = TextAlign.Center)
                        Button(
                            onClick = onRetry,
                            colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                        ) {
                            Text(store.t("retry"))
                        }
                    }
                }
            }
            preview != null -> {
                Column(
                    modifier = Modifier
                        .fillMaxSize()
                        .verticalScroll(rememberScrollState())
                        .padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    if (preview.truncated == true) {
                        val sizeStr = formatByteSize((preview.content?.toByteArray(Charsets.UTF_8)?.size ?: 0).toLong())
                        Surface(color = p.s1, shape = RoundedCornerShape(10.dp), modifier = Modifier.fillMaxWidth()) {
                            Text(
                                text = store.t("fileTruncatedHint", mapOf("size" to sizeStr)),
                                color = p.text2,
                                fontSize = PebbleFont.secondary,
                                modifier = Modifier.padding(10.dp),
                            )
                        }
                    }

                    when {
                        preview.kind == "image" && imageUrl != null -> {
                            SubcomposeAsyncImage(
                                model = imageUrl,
                                contentDescription = preview.name,
                                contentScale = ContentScale.Fit,
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .heightIn(min = 120.dp, max = 480.dp)
                                    .clip(RoundedCornerShape(12.dp)),
                                loading = {
                                    Box(modifier = Modifier.fillMaxWidth().height(160.dp), contentAlignment = Alignment.Center) {
                                        CircularProgressIndicator(color = p.accent)
                                    }
                                },
                                error = {
                                    Text(store.t("downloadFailed"), color = p.pinkInk, fontSize = PebbleFont.secondary)
                                },
                            )
                        }
                        preview.kind == "markdown" -> {
                            MarkdownText(text = preview.content.orEmpty())
                        }
                        preview.kind == "text" -> {
                            SelectionContainer {
                                Box(modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())) {
                                    Text(
                                        text = preview.content.orEmpty(),
                                        fontFamily = MonoFont,
                                        fontSize = PebbleFont.secondary,
                                        lineHeight = 20.sp,
                                        color = p.text,
                                    )
                                }
                            }
                        }
                        else -> {
                            Column(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .padding(vertical = 32.dp),
                                horizontalAlignment = Alignment.CenterHorizontally,
                                verticalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                Icon(painterResource(PebbleIcons.FolderZip), contentDescription = null, tint = p.mute, modifier = Modifier.size(40.dp))
                                Text(preview.name, color = p.text, fontSize = PebbleFont.title, fontWeight = FontWeight.Bold)
                                Text(
                                    text = "${preview.mimeType} · ${formatByteSize(preview.size)}",
                                    color = p.text2,
                                    fontSize = PebbleFont.secondary,
                                    fontFamily = MonoFont,
                                )
                                Text(
                                    text = store.t("binaryFileHint"),
                                    color = p.text2,
                                    fontSize = PebbleFont.secondary,
                                    textAlign = TextAlign.Center,
                                )
                                Button(
                                    onClick = onShare,
                                    colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                                ) {
                                    Text(store.t("openQuickLook"))
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

private fun formatByteSize(bytes: Long): String {
    if (bytes < 1024L) return "$bytes B"
    val kb = bytes / 1024.0
    if (kb < 1024.0) return String.format(Locale.US, "%.1f KB", kb)
    return String.format(Locale.US, "%.1f MB", kb / 1024.0)
}
