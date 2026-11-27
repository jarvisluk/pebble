package ai.pebble.android.ui.desktop

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.StageTab
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons

@Composable
internal fun StageTabBar(
    store: PebbleStore,
    tabs: List<StageTab>,
    activeTabId: String,
    inCard: Boolean = false,
    onSelect: (String) -> Unit,
    onClose: (String) -> Unit,
    onNewTab: () -> Unit,
) {
    val p = LocalPalette.current

    Column(modifier = Modifier.fillMaxWidth().background(if (inCard) p.card else p.page)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(44.dp)
                .horizontalScroll(rememberScrollState())
                .padding(horizontal = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            tabs.forEachIndexed { idx, tab ->
                val active = tab.id == activeTabId
                val nextActive = idx + 1 < tabs.size && tabs[idx + 1].id == activeTabId
                val shape = RoundedCornerShape(8.dp)
                Row(
                    modifier = Modifier
                        .widthIn(max = 210.dp)
                        .clip(shape)
                        .background(if (active) p.s1 else Color.Transparent),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Row(
                        modifier = Modifier
                            .weight(1f, fill = false)
                            .clickable { onSelect(tab.id) }
                            .padding(
                                start = 10.dp,
                                end = if (tab.closable) 2.dp else 10.dp,
                                top = 5.dp,
                                bottom = 5.dp,
                            ),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Icon(
                            painter = painterResource(StageStateOps.iconFor(tab.kind)),
                            contentDescription = null,
                            tint = if (active) p.text else p.text2,
                            modifier = Modifier.size(14.dp),
                        )
                        Text(
                            text = tab.title,
                            color = if (active) p.text else p.text2,
                            fontSize = PebbleFont.secondary,
                            fontWeight = if (active) FontWeight.SemiBold else FontWeight.Medium,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    if (tab.closable) {
                        Box(
                            modifier = Modifier
                                .padding(end = 4.dp)
                                .size(20.dp)
                                .clip(CircleShape)
                                .clickable { onClose(tab.id) },
                            contentAlignment = Alignment.Center,
                        ) {
                            Icon(
                                painter = painterResource(PebbleIcons.Close),
                                contentDescription = store.t("stageCloseTab"),
                                tint = p.mute.copy(alpha = if (active) 0.9f else 0.55f),
                                modifier = Modifier.size(12.dp),
                            )
                        }
                    }
                }

                if (idx < tabs.size - 1 && !active && !nextActive) {
                    Box(
                        modifier = Modifier
                            .padding(horizontal = 2.dp)
                            .width(0.5.dp)
                            .height(14.dp)
                            .background(p.s2),
                    )
                }
            }

            Box(
                modifier = Modifier
                    .padding(start = 2.dp)
                    .size(28.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .clickable(onClick = onNewTab),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    painter = painterResource(PebbleIcons.Add),
                    contentDescription = store.t("stageOpenNewTab"),
                    tint = p.text2,
                    modifier = Modifier.size(16.dp),
                )
            }
        }
        HorizontalDivider(thickness = 0.5.dp, color = p.s2)
    }
}
