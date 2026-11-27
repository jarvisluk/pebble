package ai.pebble.android.ui.chat

import android.content.Context
import android.graphics.BitmapFactory
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.union
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.ChatMessage
import ai.pebble.android.data.MentionCandidate
import ai.pebble.android.data.MessageMedia
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.PendingAttachment
import ai.pebble.android.data.SlashCommandItem
import ai.pebble.android.data.errText
import ai.pebble.android.data.formatStamp
import ai.pebble.android.data.parseIsoInstant
import ai.pebble.android.ui.theme.BotAccent
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.TagPill
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.Duration
import java.util.UUID
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons


@Composable
internal fun androidx.compose.foundation.layout.BoxScope.ChatComposer(
    store: PebbleStore,
    botId: String,
    bot: ai.pebble.android.data.BotStatus?,
    botName: String,
    pal: ai.pebble.android.ui.theme.BotAccent,
    compact: Boolean,
    inDesktopSidebar: Boolean,
    input: String,
    sending: Boolean,
    showStop: Boolean,
    slashQuery: String?,
    filteredCommands: List<SlashCommandItem>,
    activeMentionRange: Pair<IntRange, String>?,
    mentions: List<MentionCandidate>,
    pendingAttachments: List<PendingAttachment>,
    onInputChange: (String) -> Unit,
    onUpdateInput: (String) -> Unit,
    onDismissAutocomplete: () -> Unit,
    onRemoveAttachment: (String) -> Unit,
    onPickPhotos: () -> Unit,
    onPickFiles: () -> Unit,
    onInterrupt: () -> Unit,
    onSubmit: () -> Unit,
    onError: (String) -> Unit,
    topDock: (@Composable () -> Unit)? = null,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()
    var attachMenuOpen by remember { mutableStateOf(false) }

    val showSlashAC = slashQuery != null && filteredCommands.isNotEmpty()
    val showMentionAC = activeMentionRange != null && mentions.isNotEmpty()
    val hasContent = input.trim().isNotEmpty() || pendingAttachments.isNotEmpty()
    val hasUploading = pendingAttachments.any { it.uploading }

    Column(
        modifier = Modifier
            .align(Alignment.BottomCenter)
            .widthIn(max = 840.dp)
            .fillMaxWidth()
            .background(if (inDesktopSidebar) p.card else Color.Transparent),
    ) {
        if (inDesktopSidebar) {
            HorizontalDivider(thickness = 0.5.dp, color = p.s2)
        }
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(
                    horizontal = if (inDesktopSidebar) 12.dp else if (compact) 10.dp else 14.dp,
                    vertical = if (inDesktopSidebar) 10.dp else 8.dp,
                ),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
        topDock?.invoke()
        if (showSlashAC || showMentionAC) {
            Surface(
                color = p.card,
                shape = RoundedCornerShape(16.dp),
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 220.dp)
                    .shadow(10.dp, RoundedCornerShape(16.dp)),
            ) {
                LazyColumn(contentPadding = PaddingValues(6.dp)) {
                    if (showSlashAC) {
                        itemsIndexed(filteredCommands.take(12), key = { _, c -> c.id }) { _, cmd ->
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clip(RoundedCornerShape(10.dp))
                                    .clickable {
                                        val immediate = setOf("status", "bots", "reset", "compact", "clear", "cost", "context")
                                        onDismissAutocomplete()
                                        onUpdateInput(
                                            if (cmd.name in immediate && cmd.argumentHint.isNullOrEmpty()) {
                                                "/${cmd.name}"
                                            } else {
                                                "/${cmd.name} "
                                            },
                                        )
                                    }
                                    .padding(horizontal = 12.dp, vertical = 8.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                    val hint = cmd.argumentHint?.takeIf { it.isNotEmpty() }?.let { " $it" }.orEmpty()
                                    Text(
                                        text = "/${cmd.name}$hint",
                                        fontFamily = MonoFont,
                                        fontSize = PebbleFont.body,
                                        fontWeight = FontWeight.Medium,
                                        color = p.text,
                                    )
                                    if (!cmd.description.isNullOrEmpty()) {
                                        Text(cmd.description, color = p.mute, fontSize = PebbleFont.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    }
                                }
                                TagPill(
                                    text = when (cmd.source) {
                                        "hub" -> store.t("cmdSourceHub")
                                        "skill" -> store.t("cmdSourceSkill")
                                        "workflow" -> store.t("cmdSourceWorkflow")
                                        else -> store.t("cmdSourceHarness")
                                    },
                                    bg = p.s1,
                                    fg = p.text2,
                                )
                            }
                        }
                    } else if (activeMentionRange != null) {
                        val range = activeMentionRange.first
                        itemsIndexed(mentions.take(12), key = { _, m -> m.id }) { _, item ->
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clip(RoundedCornerShape(10.dp))
                                    .clickable {
                                        onDismissAutocomplete()
                                        val before = input.substring(0, range.first)
                                        onUpdateInput("$before@${item.value} ")
                                    }
                                    .padding(horizontal = 12.dp, vertical = 8.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                    Text(
                                        text = if (item.kind == "file") item.value else "@${item.value}",
                                        fontFamily = MonoFont,
                                        fontSize = PebbleFont.body,
                                        fontWeight = FontWeight.Medium,
                                        color = p.text,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                    if (!item.description.isNullOrEmpty() && item.description != item.value) {
                                        Text(item.description, color = p.mute, fontSize = PebbleFont.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    }
                                }
                                TagPill(
                                    text = when (item.kind) {
                                        "bot" -> store.t("mentionBot")
                                        "agent" -> store.t("mentionAgent")
                                        else -> store.t("mentionFile")
                                    },
                                    bg = p.s1,
                                    fg = p.text2,
                                )
                            }
                        }
                    }
                }
            }
        }

        if (pendingAttachments.isNotEmpty()) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState()),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                pendingAttachments.forEach { att ->
                    Box(modifier = Modifier.padding(top = 6.dp, end = 6.dp)) {
                        val borderColor = if (att.error != null) p.danger else p.s2
                        if (att.isImage && att.previewBytes != null) {
                            val bmp = remember(att.id) {
                                BitmapFactory.decodeByteArray(att.previewBytes, 0, att.previewBytes.size)
                            }
                            if (bmp != null) {
                                Image(
                                    bitmap = bmp.asImageBitmap(),
                                    contentDescription = att.name,
                                    contentScale = ContentScale.Crop,
                                    modifier = Modifier
                                        .size(54.dp)
                                        .clip(RoundedCornerShape(10.dp))
                                        .border(1.dp, borderColor, RoundedCornerShape(10.dp)),
                                )
                            }
                        } else {
                            Row(
                                modifier = Modifier
                                    .widthIn(max = 180.dp)
                                    .clip(RoundedCornerShape(12.dp))
                                    .background(p.card)
                                    .border(1.dp, borderColor, RoundedCornerShape(12.dp))
                                    .padding(horizontal = 10.dp, vertical = 8.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(6.dp),
                            ) {
                                Icon(painterResource(PebbleIcons.Description), contentDescription = null, tint = p.text2, modifier = Modifier.size(15.dp))
                                Text(att.name, color = p.text, fontSize = PebbleFont.secondary, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                                if (att.uploading) {
                                    CircularProgressIndicator(modifier = Modifier.size(12.dp), strokeWidth = 1.5.dp, color = p.accent)
                                }
                            }
                        }
                        Box(
                            modifier = Modifier
                                .align(Alignment.TopEnd)
                                .size(18.dp)
                                .clip(CircleShape)
                                .background(p.text)
                                .clickable { onRemoveAttachment(att.id) },
                            contentAlignment = Alignment.Center,
                        ) {
                            Icon(painterResource(PebbleIcons.Close), contentDescription = null, tint = p.card, modifier = Modifier.size(12.dp))
                        }
                    }
                }
            }
        }

        // Floating Input Row
        val floatBg = if (inDesktopSidebar) p.s1 else if (p.dark) p.card else Color.White
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.Bottom,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Box {
                Surface(
                    color = floatBg,
                    shape = CircleShape,
                    shadowElevation = if (inDesktopSidebar) 0.dp else 3.dp,
                    modifier = Modifier
                        .size(44.dp)
                        .clip(CircleShape)
                        .border(0.5.dp, p.s2, CircleShape)
                        .clickable { attachMenuOpen = true },
                ) {
                    Box(contentAlignment = Alignment.Center) {
                        Icon(painterResource(PebbleIcons.Add), contentDescription = store.t("attachFile"), tint = p.text, modifier = Modifier.size(22.dp))
                    }
                }
                DropdownMenu(
                    expanded = attachMenuOpen,
                    onDismissRequest = { attachMenuOpen = false },
                    containerColor = p.card,
                ) {
                    DropdownMenuItem(
                        text = { Text(store.t("photoLibrary"), color = p.text) },
                        leadingIcon = { Icon(painterResource(PebbleIcons.PhotoLibrary), contentDescription = null, tint = p.text2) },
                        onClick = {
                            attachMenuOpen = false
                            onPickPhotos()
                        },
                    )
                    DropdownMenuItem(
                        text = { Text(store.t("chooseFile"), color = p.text) },
                        leadingIcon = { Icon(painterResource(PebbleIcons.Description), contentDescription = null, tint = p.text2) },
                        onClick = {
                            attachMenuOpen = false
                            onPickFiles()
                        },
                    )
                }
            }

            Surface(
                color = floatBg,
                shape = PebbleDimens.composerShape,
                shadowElevation = if (inDesktopSidebar) 0.dp else 3.dp,
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 44.dp)
                    .border(0.5.dp, p.s2, PebbleDimens.composerShape),
            ) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = 44.dp)
                        .padding(horizontal = 16.dp, vertical = 10.dp),
                    contentAlignment = Alignment.CenterStart,
                ) {
                    if (input.isEmpty()) {
                        Text(
                            text = store.t("msgBotPh", mapOf("name" to botName)),
                            color = p.mute,
                            fontSize = PebbleFont.body,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    BasicTextField(
                        value = input,
                        onValueChange = { onInputChange(it) },
                        textStyle = TextStyle(color = p.text, fontSize = PebbleFont.body, lineHeight = 22.sp),
                        cursorBrush = SolidColor(pal.accent),
                        maxLines = 6,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }

            val permModes = store.supportedPermissionModes(bot?.harness)
            if (permModes.size > 1) {
                val activePermMode = store.clampPermissionMode(bot?.permissionMode, bot?.harness)
                var permMenuOpen by remember { mutableStateOf(false) }
                Box {
                    Surface(
                        color = if (activePermMode == "plan") p.lilac else floatBg,
                        shape = CircleShape,
                        shadowElevation = if (inDesktopSidebar) 0.dp else 3.dp,
                        modifier = Modifier
                            .height(44.dp)
                            .clip(CircleShape)
                            .border(0.5.dp, if (activePermMode == "plan") p.lilacInk.copy(alpha = 0.4f) else p.s2, CircleShape)
                            .clickable { permMenuOpen = true },
                    ) {
                        Row(
                            modifier = Modifier.padding(horizontal = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(2.dp),
                        ) {
                            Text(
                                text = store.t("permMode_$activePermMode"),
                                color = if (activePermMode == "plan") p.lilacInk else p.text2,
                                fontSize = PebbleFont.caption,
                                fontWeight = FontWeight.Medium,
                                maxLines = 1,
                            )
                            Icon(
                                painter = painterResource(PebbleIcons.ExpandMore),
                                contentDescription = store.t("permMode"),
                                tint = if (activePermMode == "plan") p.lilacInk else p.mute,
                                modifier = Modifier.size(14.dp),
                            )
                        }
                    }
                    DropdownMenu(
                        expanded = permMenuOpen,
                        onDismissRequest = { permMenuOpen = false },
                        containerColor = p.card,
                    ) {
                        permModes.forEach { m ->
                            DropdownMenuItem(
                                text = { Text(store.t("permMode_$m"), color = p.text, fontSize = PebbleFont.body) },
                                trailingIcon = {
                                    if (activePermMode == m) {
                                        Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = pal.accent, modifier = Modifier.size(16.dp))
                                    }
                                },
                                onClick = {
                                    permMenuOpen = false
                                    if (m != activePermMode) {
                                        scope.launch {
                                            runCatching { store.updateBot(botId, mapOf("permissionMode" to m)) }
                                                .onFailure { onError(errText(it, store.t("updateFailed"))) }
                                        }
                                    }
                                },
                            )
                        }
                    }
                }
            }

            val sendEnabled = showStop || (hasContent && !hasUploading && !sending)
            val btnBg = when {
                showStop -> p.danger
                sendEnabled -> pal.accent
                else -> p.s2
            }
            val btnFg = when {
                showStop -> Color.White
                sendEnabled -> pal.onAccent
                else -> p.mute
            }
            Surface(
                color = btnBg,
                shape = CircleShape,
                shadowElevation = if (sendEnabled) 3.dp else 0.dp,
                modifier = Modifier
                    .size(44.dp)
                    .clip(CircleShape)
                    .clickable(enabled = sendEnabled) {
                        if (showStop) onInterrupt() else onSubmit()
                    },
            ) {
                Box(contentAlignment = Alignment.Center) {
                    Icon(
                        painter = painterResource(if (showStop) PebbleIcons.Stop else PebbleIcons.ArrowUpward),
                        contentDescription = store.t(if (showStop) "stopOutput" else "sendMsg"),
                        tint = btnFg,
                        modifier = Modifier.size(if (showStop) 18.dp else 22.dp),
                    )
                }
            }
        }
        }
    }
}

internal data class PickedPayload(val name: String, val mime: String, val isImage: Boolean, val bytes: ByteArray)

internal fun readUriPayload(context: Context, uri: Uri, forceImage: Boolean): PickedPayload? {
    val resolver = context.contentResolver
    val mime = resolver.getType(uri) ?: if (forceImage) "image/jpeg" else "application/octet-stream"
    var name = ""
    runCatching {
        resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                val idx = c.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                if (idx >= 0) name = c.getString(idx).orEmpty()
            }
        }
    }
    if (name.isBlank()) {
        name = if (forceImage) "photo_${System.currentTimeMillis()}.jpg" else "file_${System.currentTimeMillis()}"
    }
    val bytes = resolver.openInputStream(uri)?.use { it.readBytes() } ?: return null
    val isImg = forceImage || mime.startsWith("image/")
    return PickedPayload(name = name, mime = mime, isImage = isImg, bytes = bytes)
}
