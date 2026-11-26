package ai.pebble.android.ui.chat

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.StageKind
import ai.pebble.android.data.StageRef
import ai.pebble.android.ui.desktop.StageStateOps
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons

@Composable
internal fun StageJumpCard(
    store: PebbleStore,
    ref: StageRef,
    inSidebar: Boolean = false,
    onOpen: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val p = LocalPalette.current
    val kind = ref.resolvedKind
    val titleText = ref.title?.trim().orEmpty().ifEmpty {
        when (kind) {
            StageKind.FILES -> store.t("stageFilesTab")
            StageKind.DESKTOP -> store.t("stageDesktopTab")
            else -> StageStateOps.basename(ref.path).ifEmpty { ref.kind }
        }
    }
    val subtitleText = ref.subtitle?.trim().orEmpty().ifEmpty {
        ref.path?.takeIf { it.isNotBlank() }
            ?: ref.url?.takeIf { it.isNotBlank() }
            ?: ref.resourceUri?.takeIf { it.isNotBlank() }
            ?: ref.target?.takeIf { it.isNotBlank() && it != "local" }
            ?: store.t("stageCardSubmitted")
    }

    val shape = RoundedCornerShape(16.dp)
    Surface(
        color = if (inSidebar) p.s1 else p.card,
        shape = shape,
        modifier = modifier
            .widthIn(max = 360.dp)
            .clip(shape)
            .clickable(onClick = onOpen),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(32.dp)
                    .clip(RoundedCornerShape(10.dp))
                    .background(if (inSidebar) p.card else p.s1),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    painter = painterResource(StageStateOps.iconFor(kind)),
                    contentDescription = null,
                    tint = p.text,
                    modifier = Modifier.size(16.dp),
                )
            }

            Column(
                modifier = Modifier.weight(1f, fill = false),
                verticalArrangement = Arrangement.spacedBy(1.dp),
            ) {
                Text(
                    text = titleText,
                    color = p.text,
                    fontSize = PebbleFont.body,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = subtitleText,
                    color = p.mute,
                    fontSize = PebbleFont.caption,
                    maxLines = 1,
                    overflow = TextOverflow.MiddleEllipsis,
                )
            }

            Spacer(Modifier.weight(1f))

            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(3.dp),
            ) {
                Text(
                    text = store.t("stageOpenAction"),
                    color = p.text2,
                    fontSize = PebbleFont.caption,
                    fontWeight = FontWeight.SemiBold,
                )
                Icon(
                    painter = painterResource(PebbleIcons.ChevronRight),
                    contentDescription = null,
                    tint = p.text2,
                    modifier = Modifier.size(14.dp),
                )
            }
        }
    }
}
