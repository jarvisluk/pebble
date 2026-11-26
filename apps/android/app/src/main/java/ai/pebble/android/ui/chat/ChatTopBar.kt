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


@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun ChatTopBar(
    store: PebbleStore,
    botId: String,
    bot: ai.pebble.android.data.BotStatus?,
    index: Int,
    botName: String,
    compact: Boolean,
    inDesktopSidebar: Boolean,
    onBack: (() -> Unit)?,
    onOpenDesktop: (String) -> Unit,
    onOpenStage: (String) -> Unit,
    hasUnreadLatestStage: Boolean = false,
    onToggleDetail: (() -> Unit)?,
    onOpenBotInfo: (() -> Unit)?,
    onSelectBot: ((String) -> Unit)?,
    onCloseSidebar: (() -> Unit)?,
) {
    val p = LocalPalette.current
    var botSwitcherOpen by remember { mutableStateOf(false) }

    if (inDesktopSidebar) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box {
                Row(
                    modifier = Modifier
                        .clip(RoundedCornerShape(10.dp))
                        .then(if (store.bots.size > 1) Modifier.clickable { botSwitcherOpen = true } else Modifier)
                        .padding(vertical = 2.dp, horizontal = 2.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    BotAvatar(
                        id = botId,
                        size = 24.dp,
                        index = index,
                        avatar = bot?.avatar,
                        color = bot?.color,
                        busy = bot?.isBusy == true,
                        interactive = true,
                    )
                    Text(
                        text = botName,
                        color = p.text,
                        fontSize = PebbleFont.cardTitle,
                        fontWeight = FontWeight.SemiBold,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f, fill = false),
                    )
                    if (store.bots.size > 1) {
                        Icon(painterResource(PebbleIcons.KeyboardArrowDown), contentDescription = null, tint = p.mute, modifier = Modifier.size(16.dp))
                    }
                }
                DropdownMenu(
                    expanded = botSwitcherOpen,
                    onDismissRequest = { botSwitcherOpen = false },
                    containerColor = p.card,
                ) {
                    store.bots.forEach { b ->
                        DropdownMenuItem(
                            text = { Text(b.displayName, color = p.text) },
                            trailingIcon = {
                                if (b.id == botId) {
                                    Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp))
                                }
                            },
                            onClick = {
                                botSwitcherOpen = false
                                onSelectBot?.invoke(b.id)
                            },
                        )
                    }
                }
            }
            Spacer(Modifier.weight(1f))
            if (onCloseSidebar != null) {
                IconButton(onClick = onCloseSidebar, modifier = Modifier.size(28.dp)) {
                    Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("close"), tint = p.text, modifier = Modifier.size(18.dp))
                }
            }
        }
        HorizontalDivider(thickness = 0.5.dp, color = p.s2)
    } else {
        TopAppBar(
            colors = TopAppBarDefaults.topAppBarColors(
                containerColor = p.page,
                titleContentColor = p.text,
            ),
            navigationIcon = {
                if (compact && onBack != null) {
                    IconButton(onClick = onBack) {
                        Icon(painterResource(PebbleIcons.ArrowBack), contentDescription = store.t("back"), tint = p.text)
                    }
                }
            },
            title = {
                Row(
                    modifier = Modifier
                        .clip(RoundedCornerShape(10.dp))
                        .then(if (onOpenBotInfo != null) Modifier.clickable { onOpenBotInfo() } else Modifier)
                        .padding(vertical = 4.dp, horizontal = 2.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    BotAvatar(
                        id = botId,
                        size = 28.dp,
                        index = index,
                        avatar = bot?.avatar,
                        color = bot?.color,
                        busy = bot?.isBusy == true,
                        interactive = true,
                    )
                    Text(
                        text = botName,
                        fontSize = PebbleFont.title,
                        fontWeight = FontWeight.Bold,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f, fill = false),
                    )
                }
            },
            actions = {
                IconButton(onClick = { onOpenStage(botId) }) {
                    Box {
                        Icon(
                            painter = painterResource(PebbleIcons.ViewSidebar),
                            contentDescription = store.t(if (bot?.latestStage != null) "stageLatestTab" else "stageOpenWorkspace"),
                            tint = p.text,
                        )
                        if (hasUnreadLatestStage) {
                            Box(
                                modifier = Modifier
                                    .align(Alignment.TopEnd)
                                    .size(7.dp)
                                    .clip(CircleShape)
                                    .background(p.busy),
                            )
                        }
                    }
                }
                IconButton(onClick = {
                    val toTarget = store.takeovers.firstOrNull { it.botId == botId }?.target?.takeIf { it.isNotEmpty() }
                        ?: bot?.desktopTarget?.takeIf { it.isNotEmpty() }
                        ?: store.takeovers.firstOrNull()?.target?.takeIf { it.isNotEmpty() }
                        ?: store.activeDesktopTarget.target.takeIf { it.isNotEmpty() }
                        ?: "local"
                    onOpenDesktop(toTarget)
                }) {
                    Icon(
                        painter = painterResource(if (store.takeovers.isEmpty()) PebbleIcons.DesktopWindows else PebbleIcons.Warning),
                        contentDescription = store.t("remoteDesktop"),
                        tint = if (store.takeovers.isEmpty()) p.text else p.busy,
                    )
                }
                if (onToggleDetail != null) {
                    IconButton(onClick = onToggleDetail) {
                        Icon(painterResource(PebbleIcons.FormatListBulleted), contentDescription = store.t("rightPanelTitle"), tint = p.text)
                    }
                }
            },
        )
    }
}

@Composable
internal fun ChatStatusBanners(
    store: PebbleStore,
    pal: ai.pebble.android.ui.theme.BotAccent,
    inDesktopSidebar: Boolean,
    onOpenDesktop: (String) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    if (!inDesktopSidebar && store.usingStandby) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .background(p.s1)
                .padding(horizontal = 16.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Icon(painterResource(PebbleIcons.SyncProblem), contentDescription = null, tint = p.text2, modifier = Modifier.size(16.dp))
            Text(store.t("standbyActiveBanner"), color = p.text, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
            TextButton(
                onClick = { scope.launch { store.reconnect() } },
                contentPadding = PaddingValues(horizontal = 8.dp),
            ) {
                Text(store.t("retryPrimary"), fontSize = PebbleFont.secondary, color = p.accent)
            }
        }
    }
    if (!inDesktopSidebar && store.activeDesktopTarget.temporary) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .background(p.s1)
                .padding(horizontal = 16.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Icon(painterResource(PebbleIcons.Computer), contentDescription = null, tint = p.text2, modifier = Modifier.size(16.dp))
            Text(
                text = store.t(
                    "tempDesktopBanner",
                    mapOf("name" to (store.activeDesktopTarget.name ?: store.activeDesktopTarget.target)),
                ),
                color = p.text,
                fontSize = PebbleFont.secondary,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            TextButton(
                onClick = { onOpenDesktop(store.activeDesktopTarget.target) },
                contentPadding = PaddingValues(horizontal = 6.dp),
            ) {
                Text(store.t("desktopShort"), fontSize = PebbleFont.secondary, color = p.accent)
            }
            TextButton(
                onClick = { scope.launch { runCatching { store.setActiveDesktopTarget("local") } } },
                contentPadding = PaddingValues(horizontal = 6.dp),
            ) {
                Text(store.t("switchBackLocal"), fontSize = PebbleFont.secondary, color = p.accent)
            }
        }
    }
    val to = store.takeovers.firstOrNull()
    if (to != null) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .background(p.pink)
                .padding(horizontal = 16.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Icon(painterResource(PebbleIcons.PanTool), contentDescription = null, tint = p.pinkInk, modifier = Modifier.size(16.dp))
            Text(
                text = store.t("takeoverInline", mapOf("reason" to to.reason.orEmpty())),
                color = p.pinkInk,
                fontSize = PebbleFont.body,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            OutlinedButton(
                onClick = { onOpenDesktop(to.target ?: "local") },
                contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
            ) {
                Text(store.t("handleAction"), fontSize = PebbleFont.secondary, color = p.pinkInk)
            }
            Button(
                onClick = { scope.launch { runCatching { store.resolveTakeover(to.id) } } },
                colors = ButtonDefaults.buttonColors(containerColor = pal.accent, contentColor = pal.onAccent),
                contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
            ) {
                Text(store.t("resumeShort"), fontSize = PebbleFont.secondary)
            }
        }
    }
}
