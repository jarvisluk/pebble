package ai.pebble.android.ui.settings

import android.Manifest
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
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
import ai.pebble.android.notify.NotificationHelper
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader

@Composable
fun GeneralSettingsTab(store: PebbleStore) {
    val p = LocalPalette.current
    val context = LocalContext.current

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
                "zh-CN" to "简体中文",
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
