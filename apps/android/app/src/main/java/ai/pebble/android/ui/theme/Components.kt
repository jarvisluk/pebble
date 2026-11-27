package ai.pebble.android.ui.theme

import ai.pebble.android.ui.theme.PebbleFont
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.i18n.L
import androidx.compose.ui.res.painterResource

data class ConfirmItem(
    val title: String,
    val message: String,
    val destructive: Boolean = true,
    val onConfirm: () -> Unit,
)

/**
 * 按钮加载态：保留原按钮与内容占位，仅隐藏内容并原位叠加转圈。
 * 禁止把按钮整体替换为裸 CircularProgressIndicator，否则顶栏 / 行内按钮宽度跳动、相邻元素位移。
 */
@Composable
fun LoadingContent(loading: Boolean, color: Color, content: @Composable () -> Unit) {
    Box(contentAlignment = Alignment.Center) {
        Box(Modifier.alpha(if (loading) 0f else 1f)) { content() }
        if (loading) {
            CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = color)
        }
    }
}

fun copyToClipboard(context: Context, text: String) {
    val cm = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager ?: return
    cm.setPrimaryClip(ClipData.newPlainText("pebble", text))
}

fun readClipboardText(context: Context): String {
    val cm = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager ?: return ""
    val clip = cm.primaryClip ?: return ""
    if (clip.itemCount <= 0) return ""
    return clip.getItemAt(0).coerceToText(context)?.toString().orEmpty()
}

@Composable
fun ConfirmAlertDialog(
    item: ConfirmItem?,
    onDismiss: () -> Unit,
) {
    val p = LocalPalette.current
    if (item == null) return
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = p.card,
        titleContentColor = p.text,
        textContentColor = p.text2,
        title = { Text(item.title, fontSize = PebbleFont.title, fontWeight = FontWeight.Bold) },
        text = { Text(item.message, fontSize = PebbleFont.body) },
        confirmButton = {
            TextButton(
                onClick = {
                    val act = item.onConfirm
                    onDismiss()
                    act()
                },
                colors = ButtonDefaults.textButtonColors(
                    contentColor = if (item.destructive) p.danger else p.accent,
                ),
            ) {
                Text(L.t("confirm"), fontWeight = FontWeight.SemiBold)
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(L.t("cancel"), color = p.text2)
            }
        },
    )
}

@Composable
fun TagPill(
    text: String,
    bg: Color,
    fg: Color,
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier,
        color = bg,
        shape = CircleShape,
    ) {
        Text(
            text = text,
            color = fg,
            fontSize = PebbleFont.secondary,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
        )
    }
}

@Composable
fun StatusTextBanner(
    text: String,
    error: Boolean = true,
    modifier: Modifier = Modifier,
) {
    val p = LocalPalette.current
    val fg = if (error) p.danger else p.sageInk
    Row(
        modifier = modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(
            painter = painterResource(if (error) PebbleIcons.Warning else PebbleIcons.CheckCircle),
            contentDescription = null,
            tint = fg,
            modifier = Modifier.size(18.dp),
        )
        Text(
            text = text,
            color = fg,
            fontSize = PebbleFont.body,
        )
    }
}

@Composable
fun SectionHeader(
    title: String,
    modifier: Modifier = Modifier,
    trailing: (@Composable () -> Unit)? = null,
) {
    val p = LocalPalette.current
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = title,
            color = p.text2,
            fontSize = PebbleFont.secondary,
            fontWeight = FontWeight.SemiBold,
            modifier = Modifier.weight(1f),
        )
        trailing?.invoke()
    }
}

@Composable
fun SectionFooter(
    text: String,
    color: Color? = null,
    modifier: Modifier = Modifier,
) {
    val p = LocalPalette.current
    Text(
        text = text,
        color = color ?: p.mute,
        fontSize = PebbleFont.secondary,
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
    )
}

@Composable
fun SectionCard(
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    val p = LocalPalette.current
    Surface(
        modifier = modifier.fillMaxWidth(),
        color = p.card,
        shape = PebbleDimens.cardShape,
    ) {
        Column(
            modifier = Modifier.fillMaxWidth(),
            content = content,
        )
    }
}

@Composable
fun CardDivider() {
    val p = LocalPalette.current
    HorizontalDivider(
        modifier = Modifier.padding(horizontal = 16.dp),
        thickness = 0.5.dp,
        color = p.s2,
    )
}

@Composable
fun pebbleFieldColors() = LocalPalette.current.let { p ->
    OutlinedTextFieldDefaults.colors(
        focusedContainerColor = p.s1,
        unfocusedContainerColor = p.s1,
        disabledContainerColor = p.s1,
        focusedBorderColor = p.accent,
        unfocusedBorderColor = Color.Transparent,
        focusedTextColor = p.text,
        unfocusedTextColor = p.text,
        focusedPlaceholderColor = p.mute,
        unfocusedPlaceholderColor = p.mute,
        cursorColor = p.accent,
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun BotAppearancePicker(
    botId: String,
    avatar: String,
    color: String,
    onAvatarChange: (String) -> Unit,
    onColorChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    index: Int = -1,
    accent: Color? = null,
) {
    val p = LocalPalette.current
    val ring = accent ?: p.accent
    val effectiveAvatar = normalizeBotAvatar(avatar)
    val effectiveHex = botColorHex(botId, index, color)
    val hasCustom = effectiveAvatar != "blob" || normalizeBotHex(color) != null

    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = L.t("avatarShape"),
                color = p.text2,
                fontSize = PebbleFont.secondary,
                modifier = Modifier.weight(1f),
            )
            if (hasCustom) {
                Text(
                    text = L.t("resetDefault"),
                    color = ring,
                    fontSize = PebbleFont.secondary,
                    fontWeight = FontWeight.Medium,
                    modifier = Modifier
                        .clip(RoundedCornerShape(6.dp))
                        .clickable {
                            onAvatarChange("")
                            onColorChange("")
                        }
                        .padding(horizontal = 6.dp, vertical = 2.dp),
                )
            }
        }

        // 4 x 2 avatar grid
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            BotAvatars.chunked(4).forEach { rowItems ->
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    rowItems.forEach { kind ->
                        val active = effectiveAvatar == kind
                        Box(
                            modifier = Modifier
                                .weight(1f)
                                .clip(RoundedCornerShape(10.dp))
                                .background(p.s1)
                                .then(
                                    if (active) Modifier.border(2.dp, ring, RoundedCornerShape(10.dp))
                                    else Modifier
                                )
                                .clickable { onAvatarChange(if (kind == "blob") "" else kind) }
                                .padding(vertical = 8.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            BotAvatar(
                                id = botId,
                                size = 32.dp,
                                index = index,
                                avatar = kind,
                                color = effectiveHex,
                            )
                        }
                    }
                }
            }
        }

        Text(
            text = L.t("botColorLabel"),
            color = p.text2,
            fontSize = PebbleFont.secondary,
        )

        FlowRow(
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            BotColors.forEach { hex ->
                val active = normalizeBotHex(color) == hex
                Box(
                    modifier = Modifier
                        .size(28.dp)
                        .clip(CircleShape)
                        .background(colorFromHex(hex))
                        .clickable { onColorChange(hex) },
                    contentAlignment = Alignment.Center,
                ) {
                    if (active) {
                        Icon(
                            painter = painterResource( PebbleIcons.Check),
                            contentDescription = null,
                            tint = Color.White,
                            modifier = Modifier.size(16.dp),
                        )
                    }
                }
            }
        }

        OutlinedTextField(
            value = color,
            onValueChange = { next ->
                val clean = next.trim()
                if (clean.length <= 7) onColorChange(clean)
            },
            label = { Text(L.t("customColorHex"), fontSize = PebbleFont.secondary) },
            placeholder = { Text(effectiveHex, fontFamily = MonoFont) },
            singleLine = true,
            textStyle = androidx.compose.ui.text.TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.fillMaxWidth(),
        )
    }
}
