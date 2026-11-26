package ai.pebble.android.ui.desktop

import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.WorkspaceFileContent
import ai.pebble.android.data.errText
import ai.pebble.android.ui.theme.LocalPalette
import kotlinx.coroutines.launch

@Composable
internal fun StageFilePreviewView(
    store: PebbleStore,
    botId: String,
    path: String,
    refreshTrigger: Int,
    inWideStage: Boolean,
    onQuotePath: (String) -> Unit,
    onCloseTab: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var preview by remember(botId, path) { mutableStateOf<WorkspaceFileContent?>(null) }
    var imageUrl by remember(botId, path) { mutableStateOf<String?>(null) }
    var loading by remember(botId, path) { mutableStateOf(false) }
    var error by remember(botId, path) { mutableStateOf("") }
    var seq by remember(botId, path) { mutableIntStateOf(0) }
    var downloading by remember(botId, path) { mutableStateOf(false) }
    var toastMsg by remember(botId, path) { mutableStateOf("") }

    suspend fun loadFile() {
        if (botId.isEmpty() || path.isEmpty()) return
        seq += 1
        val cur = seq
        loading = true
        error = ""
        imageUrl = null
        toastMsg = ""
        try {
            val doc = store.readWorkspaceFile(botId, path)
            if (cur != seq) return
            preview = doc
            if (doc.kind == "image") {
                val url = store.workspaceFileImageURL(botId, path)
                if (cur != seq) return
                imageUrl = url
            }
        } catch (e: Throwable) {
            if (cur != seq) return
            preview = null
            error = errText(e, store.t("opFailed"))
        }
        if (cur == seq) loading = false
    }

    fun downloadAndShare() {
        if (downloading) return
        downloading = true
        toastMsg = ""
        scope.launch {
            try {
                val fileName = preview?.name?.ifEmpty { null } ?: path.substringAfterLast('/')
                val file = store.downloadWorkspaceFileToTemp(botId, path, fileName)
                val uri = FileProvider.getUriForFile(context, "${context.packageName}.fileprovider", file)
                val intent = Intent(Intent.ACTION_VIEW).apply {
                    setDataAndType(uri, preview?.mimeType?.takeIf { it.isNotBlank() } ?: "*/*")
                    addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                }
                val chooser = Intent.createChooser(intent, fileName)
                runCatching { context.startActivity(chooser) }
                    .onFailure { toastMsg = store.t("downloadFailed") }
            } catch (e: Throwable) {
                toastMsg = errText(e, store.t("downloadFailed"))
            } finally {
                downloading = false
            }
        }
    }

    LaunchedEffect(botId, path, refreshTrigger) {
        loadFile()
    }

    val panel: @Composable () -> Unit = {
        WorkspacePreviewPanel(
            store = store,
            path = path,
            preview = preview,
            imageUrl = imageUrl,
            loading = loading,
            error = error,
            downloading = downloading,
            toastMsg = toastMsg,
            isSplit = true,
            inCard = inWideStage,
            onBack = onCloseTab,
            onRetry = { scope.launch { loadFile() } },
            onQuotePath = { onQuotePath(path) },
            onShare = { downloadAndShare() },
        )
    }

    if (inWideStage) {
        Surface(
            color = p.card,
            shape = RoundedCornerShape(20.dp),
            modifier = modifier
                .fillMaxSize()
                .padding(12.dp),
        ) {
            panel()
        }
    } else {
        Box(
            modifier = modifier
                .fillMaxSize()
                .background(p.page),
        ) {
            panel()
        }
    }
}
