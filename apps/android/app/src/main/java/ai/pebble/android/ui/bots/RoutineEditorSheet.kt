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


private enum class RoutineMode { INTERVAL, DAILY, CRON, WEBHOOK }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun RoutineEditorSheet(
    store: PebbleStore,
    botId: String,
    routine: RoutineItem?,
    onDismiss: () -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    val hasEvery = (routine?.everyMinutes ?: 0) > 0
    val hasDaily = !routine?.dailyAt.isNullOrEmpty()
    val hasCron = !routine?.cron.isNullOrEmpty()
    val initialMode = when {
        hasCron -> RoutineMode.CRON
        hasDaily && !hasEvery -> RoutineMode.DAILY
        !hasEvery && !hasDaily && routine?.webhookTrigger == true -> RoutineMode.WEBHOOK
        else -> RoutineMode.INTERVAL
    }

    var name by remember { mutableStateOf(routine?.name.orEmpty()) }
    var mode by remember { mutableStateOf(initialMode) }
    var every by remember { mutableStateOf(if (hasEvery) "${routine?.everyMinutes}" else "60") }
    var daily by remember { mutableStateOf(if (hasDaily) routine.dailyAt.orEmpty() else "09:00") }
    var cron by remember { mutableStateOf(if (hasCron) routine.cron.orEmpty() else "0 9 * * 1-5") }
    var webhookTrigger by remember { mutableStateOf(routine?.webhookTrigger == true || initialMode == RoutineMode.WEBHOOK) }
    var secret by remember { mutableStateOf("") }
    val secretSet = routine?.webhookSecretSet == true
    var clearSecret by remember { mutableStateOf(false) }
    var webhookUrl by remember { mutableStateOf(routine?.webhookUrl.orEmpty()) }
    var delivery by remember { mutableStateOf(routine?.mainDelivery ?: "receipt") }
    var permMode by remember {
        mutableStateOf(
            when (routine?.permissionMode) {
                "auto", "plan" -> routine.permissionMode
                else -> ""
            },
        )
    }
    var prompt by remember { mutableStateOf(routine?.prompt.orEmpty()) }
    var enabled by remember { mutableStateOf(routine?.enabled ?: true) }
    var saving by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf("") }

    fun save() {
        val n = name.trim()
        val pr = prompt.trim()
        if (n.isEmpty()) { err = store.t("rtNameRequired"); return }
        if (pr.isEmpty()) { err = store.t("rtPromptRequired"); return }
        val everyMinutes = if (mode == RoutineMode.INTERVAL) (every.trim().toIntOrNull() ?: 0) else 0
        val dailyAt = if (mode == RoutineMode.DAILY) daily.trim() else ""
        val cronExpr = if (mode == RoutineMode.CRON) cron.trim() else ""
        if (mode == RoutineMode.INTERVAL && everyMinutes <= 0) { err = store.t("rtEveryPositive"); return }
        if (mode == RoutineMode.DAILY && !Regex("^\\d{2}:\\d{2}$").matches(dailyAt)) { err = store.t("rtTimeFormatErr"); return }
        if (mode == RoutineMode.CRON && cronExpr.isEmpty()) { err = store.t("rtCronRequired"); return }

        err = ""
        val body = mutableMapOf<String, Any?>(
            "name" to n,
            "everyMinutes" to everyMinutes,
            "dailyAt" to dailyAt,
            "cron" to cronExpr,
            "webhookTrigger" to (mode == RoutineMode.WEBHOOK || webhookTrigger),
            "webhookUrl" to webhookUrl.trim(),
            "prompt" to pr,
            "mainDelivery" to delivery,
            "permissionMode" to permMode,
            "enabled" to enabled,
        )
        val sec = secret.trim()
        if (clearSecret) body["clearWebhookSecret"] = true else if (sec.isNotEmpty()) body["webhookSecret"] = sec

        saving = true
        scope.launch {
            try {
                if (routine != null) store.updateRoutine(botId, routine.id, body)
                else store.createRoutine(botId, body)
                onDismiss()
            } catch (e: Throwable) {
                err = errText(e, store.t("rtSaveFailed"))
            } finally {
                saving = false
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .imePadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onDismiss) { Text(store.t("cancel"), color = p.text2) }
            Text(
                text = routine?.name ?: store.t("routines"),
                color = p.text,
                fontSize = PebbleFont.title,
                fontWeight = FontWeight.Bold,
                textAlign = TextAlign.Center,
                modifier = Modifier.weight(1f),
            )
            if (saving) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.accent)
            } else {
                TextButton(onClick = { save() }) {
                    Text(store.t("rtSaveBtn"), color = p.accent, fontWeight = FontWeight.SemiBold)
                }
            }
        }

        if (err.isNotEmpty()) StatusTextBanner(text = err, error = true)

        OutlinedTextField(
            value = name,
            onValueChange = { name = it },
            placeholder = { Text(store.t("rtNamePh")) },
            singleLine = true,
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.fillMaxWidth(),
        )

        SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
            RoutineMode.entries.forEachIndexed { idx, m ->
                val lbl = when (m) {
                    RoutineMode.INTERVAL -> store.t("rtModeInterval")
                    RoutineMode.DAILY -> store.t("rtModeDaily")
                    RoutineMode.CRON -> store.t("rtModeCron")
                    RoutineMode.WEBHOOK -> store.t("rtModeWebhook")
                }
                SegmentedButton(
                    selected = mode == m,
                    onClick = {
                        mode = m
                        if (m == RoutineMode.WEBHOOK) webhookTrigger = true
                    },
                    shape = SegmentedButtonDefaults.itemShape(idx, RoutineMode.entries.size),
                    label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                )
            }
        }

        when (mode) {
            RoutineMode.INTERVAL -> OutlinedTextField(
                value = every,
                onValueChange = { every = it },
                placeholder = { Text(store.t("rtEveryPh")) },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                singleLine = true,
                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            RoutineMode.DAILY -> OutlinedTextField(
                value = daily,
                onValueChange = { daily = it },
                placeholder = { Text(store.t("rtDailyPh")) },
                singleLine = true,
                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            RoutineMode.CRON -> OutlinedTextField(
                value = cron,
                onValueChange = { cron = it },
                placeholder = { Text(store.t("rtCronPh")) },
                singleLine = true,
                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            RoutineMode.WEBHOOK -> Unit
        }

        if (mode != RoutineMode.WEBHOOK) {
            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(store.t("rtAllowWebhook"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                Switch(checked = webhookTrigger, onCheckedChange = { webhookTrigger = it })
            }
        }

        if (mode == RoutineMode.WEBHOOK || webhookTrigger) {
            OutlinedTextField(
                value = secret,
                onValueChange = { secret = it },
                enabled = !clearSecret,
                placeholder = {
                    Text(if (secretSet && !clearSecret) store.t("rtWebhookSecretSetPh") else store.t("rtWebhookSecretPh"))
                },
                visualTransformation = PasswordVisualTransformation(),
                singleLine = true,
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            if (secretSet) {
                Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Text(store.t("rtWebhookClearSecret"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                    Switch(checked = clearSecret, onCheckedChange = { clearSecret = it; if (it) secret = "" })
                }
            }
        }

        OutlinedTextField(
            value = webhookUrl,
            onValueChange = { webhookUrl = it },
            placeholder = { Text(store.t("rtWebhookUrlPh"), fontFamily = MonoFont) },
            singleLine = true,
            textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.fillMaxWidth(),
        )

        val delivs = listOf(
            "receipt" to store.t("rtDeliveryReceipt"),
            "notify" to store.t("rtDeliveryNotify"),
            "none" to store.t("rtDeliveryNone"),
        )
        SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
            delivs.forEachIndexed { idx, (key, lbl) ->
                SegmentedButton(
                    selected = delivery == key,
                    onClick = { delivery = key },
                    shape = SegmentedButtonDefaults.itemShape(idx, delivs.size),
                    label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                )
            }
        }

        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(store.t("rtPermMode"), color = p.text2, fontSize = PebbleFont.secondary)
            val rtPerms = listOf(
                "" to store.t("rtPermFollow"),
                "auto" to store.t("permMode_auto"),
                "plan" to store.t("permMode_plan"),
            )
            SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                rtPerms.forEachIndexed { idx, (key, lbl) ->
                    SegmentedButton(
                        selected = permMode == key,
                        onClick = { permMode = key },
                        shape = SegmentedButtonDefaults.itemShape(idx, rtPerms.size),
                        label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                    )
                }
            }
        }

        OutlinedTextField(
            value = prompt,
            onValueChange = { prompt = it },
            placeholder = { Text(store.t("rtPromptPh")) },
            minLines = 3,
            maxLines = 10,
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.fillMaxWidth(),
        )

        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(store.t("rtEnabled"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
            Switch(checked = enabled, onCheckedChange = { enabled = it })
        }

        Spacer(Modifier.height(16.dp))
    }
}
