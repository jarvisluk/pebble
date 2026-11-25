package ai.pebble.android.ui.bots

import android.graphics.BitmapFactory
import android.util.Base64
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
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
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.BotStatus
import ai.pebble.android.data.HubException
import ai.pebble.android.data.MemoryItem
import ai.pebble.android.data.MemoryList
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.RoutineItem
import ai.pebble.android.data.RoutineRun
import ai.pebble.android.data.errText
import ai.pebble.android.data.resolveDefaultModel
import ai.pebble.android.ui.settings.ModelInputWithMenu
import ai.pebble.android.ui.settings.localizedMemoryKind
import ai.pebble.android.ui.settings.settingsHarnessList
import ai.pebble.android.ui.desktop.H264SurfaceView
import ai.pebble.android.ui.theme.BotAppearancePicker
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmAlertDialog
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.copyToClipboard
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.Locale
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons


@Composable
internal fun DetailRow(label: String, value: String) {
    val p = LocalPalette.current
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
        Text(value, color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
    }
}

@Composable
fun MemoryItemRow(
    store: PebbleStore,
    item: MemoryItem,
    showTags: Boolean = false,
    showScope: Boolean = false,
    onDelete: () -> Unit,
) {
    val p = LocalPalette.current
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 6.dp),
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(localizedMemoryKind(item.kind, store), color = p.sageInk, fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                if (showScope && !item.scope.isNullOrEmpty()) {
                    Text(
                        text = if (item.scope == "everywhere" || item.scope == "global") store.t("memScopeEverywhere") else store.t("memScopeProject"),
                        color = p.mute,
                        fontSize = PebbleFont.caption,
                    )
                }
                if (showTags) {
                    item.tags.orEmpty().filterNot { it.startsWith("bot:") }.forEach { tag ->
                        Text("#$tag", color = p.mute, fontSize = PebbleFont.caption)
                    }
                }
            }
            SelectionContainer {
                Text(item.text.orEmpty(), color = p.text, fontSize = PebbleFont.body)
            }
        }
        TextButton(
            onClick = onDelete,
            contentPadding = PaddingValues(horizontal = 6.dp, vertical = 2.dp),
        ) {
            Text(store.t("delete"), color = p.danger, fontSize = PebbleFont.secondary)
        }
    }
}

@Composable
internal fun RoutinesBlock(
    store: PebbleStore,
    botId: String,
    onCreate: () -> Unit,
    onEdit: (RoutineItem) -> Unit,
    onError: (String) -> Unit,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val list = store.routines[botId].orEmpty()
    var runsOpen by remember(botId) { mutableStateOf("") }
    val runsMap = remember(botId) { mutableStateMapOf<String, List<RoutineRun>>() }
    var confirm by remember(botId) { mutableStateOf<ConfirmItem?>(null) }

    ConfirmAlertDialog(item = confirm, onDismiss = { confirm = null })

    SectionHeader(
        title = store.t("routines"),
        trailing = {
            IconButton(onClick = onCreate, modifier = Modifier.size(28.dp)) {
                Icon(painterResource(PebbleIcons.Add), contentDescription = store.t("addShort"), tint = p.accent, modifier = Modifier.size(20.dp))
            }
        },
    )

    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (list.isEmpty()) {
                Text(store.t("rtEmpty"), color = p.mute, fontSize = PebbleFont.secondary)
            } else {
                list.forEachIndexed { idx, rt ->
                    if (idx > 0) CardDivider()
                    val trig = listOfNotNull(
                        (rt.everyMinutes ?: 0).takeIf { it > 0 }?.let { store.t("rtEveryShort", mapOf("n" to it)) },
                        rt.dailyAt?.takeIf { it.isNotEmpty() }?.let { store.t("rtDailyShort", mapOf("time" to it)) },
                        rt.cron?.takeIf { it.isNotEmpty() }?.let { "Cron $it" },
                        if (rt.webhookTrigger == true) store.t("rtWebhookTag") else null,
                    ).joinToString(" · ")
                    val deliv = when (rt.mainDelivery) {
                        "notify" -> store.t("rtDeliveryNotify")
                        "none" -> store.t("rtDeliveryNone")
                        else -> store.t("rtDeliveryReceipt")
                    }
                    val running = rt.running == true
                    val statusLabel = when {
                        running -> store.t("rtStatusRunning")
                        rt.lastRun?.status == "ok" -> store.t("rtStatusOk")
                        rt.lastRun?.status == "silent" -> store.t("rtStatusSilent")
                        rt.lastRun?.status == "error" -> store.t("rtStatusError")
                        else -> ""
                    }
                    val rtPermLabel = when (rt.permissionMode) {
                        "auto" -> store.t("permMode_auto")
                        "plan" -> store.t("permMode_plan")
                        else -> null
                    }
                    val meta = listOfNotNull(
                        trig.ifEmpty { null },
                        deliv,
                        rtPermLabel,
                        if (!rt.webhookUrl.isNullOrEmpty()) store.t("rtWebhookPushTag") else null,
                        rt.model?.takeIf { it.isNotEmpty() },
                    ).joinToString(" · ")
                    val isOpen = runsOpen == rt.id
                    var menuOpen by remember { mutableStateOf(false) }

                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                                    Text(rt.name ?: rt.id, color = p.text, fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    if (statusLabel.isNotEmpty()) {
                                        Text(
                                            text = statusLabel,
                                            color = if (rt.lastRun?.status == "error" && !running) p.danger else p.sageInk,
                                            fontSize = PebbleFont.secondary,
                                            fontWeight = FontWeight.SemiBold,
                                        )
                                    }
                                }
                                if (meta.isNotEmpty()) {
                                    Text(meta, color = p.text2, fontSize = PebbleFont.secondary)
                                }
                            }
                            Switch(
                                checked = rt.enabled == true,
                                onCheckedChange = { enabled ->
                                    scope.launch {
                                        runCatching { store.updateRoutine(botId, rt.id, mapOf("enabled" to enabled)) }
                                            .onFailure { onError(errText(it, store.t("rtStatusUpdateFailed"))) }
                                    }
                                },
                            )
                        }
                        Text(rt.prompt.orEmpty(), color = p.text, fontSize = PebbleFont.body, maxLines = 4, overflow = TextOverflow.Ellipsis)
                        Row(
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            OutlinedButton(
                                onClick = {
                                    scope.launch {
                                        runCatching { store.runRoutine(botId, rt.id) }
                                            .onFailure { onError(errText(it, store.t("rtRunFailed"))) }
                                    }
                                },
                                enabled = !running,
                                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                            ) {
                                Text(if (running) store.t("rtRunningBtn") else store.t("rtRunNow"), fontSize = PebbleFont.secondary, color = p.text)
                            }
                            OutlinedButton(
                                onClick = {
                                    if (isOpen) {
                                        runsOpen = ""
                                    } else {
                                        runsOpen = rt.id
                                        scope.launch {
                                            runCatching { store.fetchRoutineRuns(botId, rt.id, limit = 6) }
                                                .getOrNull()?.let { runsMap[rt.id] = it }
                                        }
                                    }
                                },
                                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                            ) {
                                Text(if (isOpen) store.t("rtRunsHide") else store.t("rtRunsBtn"), fontSize = PebbleFont.secondary, color = p.text)
                            }
                            Box {
                                IconButton(onClick = { menuOpen = true }, modifier = Modifier.size(32.dp)) {
                                    Icon(painterResource(PebbleIcons.MoreVert), contentDescription = store.t("moreActions"), tint = p.text2)
                                }
                                DropdownMenu(
                                    expanded = menuOpen,
                                    onDismissRequest = { menuOpen = false },
                                    containerColor = p.card,
                                ) {
                                    DropdownMenuItem(
                                        text = { Text(store.t("edit"), color = p.text) },
                                        onClick = {
                                            menuOpen = false
                                            onEdit(rt)
                                        },
                                    )
                                    val path = rt.webhookPath
                                    if (rt.webhookTrigger == true && !path.isNullOrEmpty()) {
                                        DropdownMenuItem(
                                            text = { Text(store.t("rtCopyWebhook"), color = p.text) },
                                            onClick = {
                                                menuOpen = false
                                                val base = store.baseURL.trimEnd('/')
                                                copyToClipboard(context, "$base$path")
                                            },
                                        )
                                    }
                                    HorizontalDivider(color = p.s2)
                                    DropdownMenuItem(
                                        text = { Text(store.t("delete"), color = p.danger) },
                                        onClick = {
                                            menuOpen = false
                                            confirm = ConfirmItem(
                                                title = store.t("rtDeleteTitle", mapOf("name" to (rt.name ?: rt.id))),
                                                message = store.t("rtDeleteDesc"),
                                            ) {
                                                scope.launch { runCatching { store.deleteRoutine(botId, rt.id) } }
                                            }
                                        },
                                    )
                                }
                            }
                        }

                        if (isOpen) {
                            val listRuns = runsMap[rt.id].orEmpty()
                            if (listRuns.isEmpty()) {
                                Text(store.t("rtRunsEmpty"), color = p.mute, fontSize = PebbleFont.secondary)
                            } else {
                                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                    listRuns.forEach { r ->
                                        val rs = when (r.status) {
                                            "ok" -> store.t("rtStatusOk")
                                            "silent" -> store.t("rtStatusSilent")
                                            else -> store.t("rtStatusError")
                                        }
                                        Column(
                                            modifier = Modifier
                                                .fillMaxWidth()
                                                .clip(RoundedCornerShape(10.dp))
                                                .background(p.s1)
                                                .padding(10.dp),
                                            verticalArrangement = Arrangement.spacedBy(2.dp),
                                        ) {
                                            Text(
                                                text = "$rs · ${r.trigger.orEmpty()} · ${String.format(Locale.US, "%.1fs", (r.durationMs ?: 0.0) / 1000.0)}",
                                                color = p.text2,
                                                fontSize = PebbleFont.caption,
                                            )
                                            if (!r.error.isNullOrEmpty()) {
                                                Text(r.error, color = p.danger, fontSize = PebbleFont.secondary)
                                            } else if (!r.output.isNullOrEmpty()) {
                                                Text(r.output, color = p.text, fontSize = PebbleFont.secondary, maxLines = 3, overflow = TextOverflow.Ellipsis)
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
