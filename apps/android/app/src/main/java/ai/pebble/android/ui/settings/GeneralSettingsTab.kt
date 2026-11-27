package ai.pebble.android.ui.settings

import android.Manifest
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import ai.pebble.android.BuildConfig
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.UpdateStatus
import ai.pebble.android.notify.NotificationHelper
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.TagPill
import kotlinx.coroutines.launch
import java.text.DateFormat
import java.time.Instant
import java.util.Date

@Composable
fun GeneralSettingsTab(store: PebbleStore) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var notifGranted by remember { mutableStateOf(NotificationHelper.hasNotificationPermission(context)) }
    val notifLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        notifGranted = granted || NotificationHelper.hasNotificationPermission(context)
    }
    val lifecycleOwner = LocalLifecycleOwner.current
    androidx.compose.runtime.DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                notifGranted = NotificationHelper.hasNotificationPermission(context)
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    SectionHeader(store.t("appearance"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            val themes = listOf(
                "system" to store.t("followSystem"),
                "light" to store.t("themeLight"),
                "dark" to store.t("themeDark"),
            )
            SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                themes.forEachIndexed { idx, (key, lbl) ->
                    SegmentedButton(
                        selected = store.themePref == key,
                        onClick = { store.updateThemePref(key) },
                        shape = SegmentedButtonDefaults.itemShape(idx, themes.size),
                        label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                    )
                }
            }

            val langs = listOf(
                "system" to store.t("followSystem"),
                "zh-CN" to "简体中文", // i18n:allow-cjk
                "en" to "English",
            )
            Text(store.t("language"), color = p.text2, fontSize = PebbleFont.secondary)
            SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                langs.forEachIndexed { idx, (key, lbl) ->
                    SegmentedButton(
                        selected = store.langPref == key,
                        onClick = { store.updateLangPref(key) },
                        shape = SegmentedButtonDefaults.itemShape(idx, langs.size),
                        label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                    )
                }
            }
        }
    }

    SectionHeader(store.t("pushSection"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(store.t("pushSystemPerm"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                PebbleSwitch(
                    checked = notifGranted,
                    onCheckedChange = { next ->
                        if (next && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                            notifLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
                        } else {
                            runCatching {
                                context.startActivity(
                                    Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                                        .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName),
                                )
                            }
                        }
                    },
                )
            }
            CardDivider()
            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(store.t("pushAndroidChannel"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                Text(
                    text = if (notifGranted) store.t("pushEnabled") else store.t("pushDisabled"),
                    color = if (notifGranted) p.sageInk else p.mute,
                    fontSize = PebbleFont.secondary,
                )
            }
        }
    }

    if (store.isConnected && store.supports("update.check")) {
        LaunchedEffect(Unit) {
            if (store.updateStatus == null) {
                store.refreshUpdateStatus()
            }
        }
        val st = store.updateStatus
        val checking = store.updateChecking || st?.state == "checking"
        val hasUpdate = store.effectiveHasUpdate
        val latest = st?.latest
        val checkedText = formatCheckedAt(st?.checkedAt)
        val autoCheck = st?.config?.autoCheck ?: true
        val interval = st?.config?.interval ?: "6h"
        val channel = st?.config?.channel ?: "stable"
        var notesExpanded by remember { mutableStateOf(false) }
        var intervalMenuOpen by remember { mutableStateOf(false) }

        SectionHeader(store.t("updateSection"))
        SectionCard {
            Column(
                modifier = Modifier.padding(14.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Box(modifier = Modifier.weight(1f)) {
                        when {
                            hasUpdate && latest != null -> {
                                val verLabel = latest.tag.ifBlank { "v${latest.version}" }
                                TagPill(
                                    text = store.t("updateAvailableTag", mapOf("version" to verLabel)),
                                    bg = p.sage,
                                    fg = p.sageInk,
                                )
                            }
                            st?.state == "error" -> {
                                Text(
                                    text = updateErrorText(store, st),
                                    color = p.danger,
                                    fontSize = PebbleFont.secondary,
                                )
                            }
                            else -> {
                                Text(
                                    text = if (checkedText.isEmpty()) store.t("updateNeverChecked") else store.t("updateUpToDate"),
                                    color = p.text2,
                                    fontSize = PebbleFont.secondary,
                                )
                            }
                        }
                    }
                    if (checking) {
                        CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp, color = p.accent)
                    } else {
                        TextButton(
                            onClick = { scope.launch { store.checkForUpdates() } },
                            contentPadding = PaddingValues(horizontal = 8.dp),
                        ) {
                            Text(
                                text = store.t("updateCheckBtn"),
                                color = p.accent,
                                fontSize = PebbleFont.secondary,
                                fontWeight = FontWeight.SemiBold,
                            )
                        }
                    }
                }

                if (checkedText.isNotEmpty()) {
                    Text(
                        text = store.t("updateLastChecked", mapOf("time" to checkedText)),
                        color = p.mute,
                        fontSize = PebbleFont.caption,
                    )
                }

                if (hasUpdate && latest != null && latest.url.isNotBlank()) {
                    CardDivider()
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(PebbleDimens.rowShape)
                            .clickable {
                                runCatching {
                                    context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(latest.url)))
                                }
                            }
                            .padding(vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Icon(painterResource(PebbleIcons.ArrowOutward), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                        Text(store.t("updateViewRelease"), color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
                    }
                }

                val notes = latest?.notes.orEmpty().trim()
                if (hasUpdate && notes.isNotEmpty()) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(PebbleDimens.rowShape)
                            .clickable { notesExpanded = !notesExpanded }
                            .padding(vertical = 4.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            text = store.t("updateReleaseNotes"),
                            color = p.text2,
                            fontSize = PebbleFont.secondary,
                            fontWeight = FontWeight.Medium,
                            modifier = Modifier.weight(1f),
                        )
                        Icon(
                            painter = painterResource(if (notesExpanded) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                            contentDescription = null,
                            tint = p.text2,
                            modifier = Modifier.size(18.dp),
                        )
                    }
                    if (notesExpanded) {
                        Text(
                            text = notes,
                            color = p.text2,
                            fontSize = PebbleFont.caption,
                            fontFamily = MonoFont,
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(PebbleDimens.rowShape)
                                .background(p.s1)
                                .padding(10.dp),
                        )
                    }
                }

                if (st != null) {
                    CardDivider()
                    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text(store.t("updateAutoCheck"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                        PebbleSwitch(
                            checked = autoCheck,
                            onCheckedChange = { next ->
                                scope.launch { store.saveUpdateConfig(autoCheck = next) }
                            },
                        )
                    }

                    if (autoCheck) {
                        Box(modifier = Modifier.fillMaxWidth()) {
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clip(PebbleDimens.rowShape)
                                    .clickable { intervalMenuOpen = true }
                                    .padding(vertical = 6.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Text(store.t("updateInterval"), color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                Text(store.t("updateInterval_$interval"), color = p.text, fontSize = PebbleFont.secondary)
                                Icon(painterResource(PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                            }
                            DropdownMenu(
                                expanded = intervalMenuOpen,
                                onDismissRequest = { intervalMenuOpen = false },
                                containerColor = p.card,
                            ) {
                                listOf("30m", "1h", "6h", "24h").forEach { iv ->
                                    DropdownMenuItem(
                                        text = { Text(store.t("updateInterval_$iv"), color = p.text, fontSize = PebbleFont.body) },
                                        onClick = {
                                            intervalMenuOpen = false
                                            scope.launch { store.saveUpdateConfig(interval = iv) }
                                        },
                                    )
                                }
                            }
                        }
                    }

                    val channels = listOf(
                        "stable" to store.t("updateChannel_stable"),
                        "prerelease" to store.t("updateChannel_prerelease"),
                    )
                    Text(store.t("updateChannel"), color = p.text2, fontSize = PebbleFont.secondary)
                    SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                        channels.forEachIndexed { idx, (key, lbl) ->
                            SegmentedButton(
                                selected = channel == key,
                                onClick = { scope.launch { store.saveUpdateConfig(channel = key) } },
                                shape = SegmentedButtonDefaults.itemShape(idx, channels.size),
                                label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                            )
                        }
                    }
                }
            }
        }
    }

    SectionHeader(store.t("aboutSection"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(store.t("appVersionLbl"), color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                Text(BuildConfig.VERSION_NAME, color = p.text, fontSize = PebbleFont.secondary, fontFamily = MonoFont)
            }
            CardDivider()
            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(store.t("serverVersionLbl"), color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                Text(store.serverInfo?.version ?: "—", color = p.text, fontSize = PebbleFont.secondary, fontFamily = MonoFont)
            }
            CardDivider()
            Text(store.t("openSourceDesc"), color = p.text2, fontSize = PebbleFont.secondary)
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .clickable {
                        runCatching {
                            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("https://github.com/jarvisluk/pebble")))
                        }
                    }
                    .padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Icon(painterResource(PebbleIcons.Star), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                Text(store.t("openSourceLinkBtn"), color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
            }
        }
    }
}

private fun updateErrorText(store: PebbleStore, st: UpdateStatus?): String = when (st?.errorCode) {
    "rate_limited" -> store.t("updateErrRateLimited")
    "network_error" -> store.t("updateErrNetwork")
    else -> st?.error?.takeIf { it.isNotBlank() } ?: store.t("updateErrGeneric")
}

private fun formatCheckedAt(iso: String?): String {
    if (iso.isNullOrBlank()) return ""
    return runCatching {
        val date = Date.from(Instant.parse(iso))
        DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(date)
    }.getOrDefault(iso)
}
