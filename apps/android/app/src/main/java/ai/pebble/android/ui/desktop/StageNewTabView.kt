package ai.pebble.android.ui.desktop

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
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
import ai.pebble.android.data.BotTask
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.chat.taskDeviceLabel
import ai.pebble.android.ui.chat.taskStatusColors
import ai.pebble.android.ui.chat.taskStatusText
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.TagPill

@Composable
internal fun StageNewTabView(
    store: PebbleStore,
    botId: String,
    canUseFiles: Boolean,
    inWideStage: Boolean,
    onOpenFiles: () -> Unit,
    onOpenDesktop: () -> Unit,
    onOpenTask: (BotTask) -> Unit,
    modifier: Modifier = Modifier,
) {
    val p = LocalPalette.current
    val allTasks = store.tasks[botId].orEmpty()
    val launcherTasks = (allTasks.filter { it.isActive } + allTasks.filter { it.isTerminal }).take(8)

    val content: @Composable () -> Unit = {
        Box(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(24.dp),
            contentAlignment = Alignment.Center,
        ) {
            Column(
                modifier = Modifier
                    .widthIn(max = 460.dp)
                    .fillMaxWidth(),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(
                        text = store.t("stageNewTab"),
                        color = p.text,
                        fontSize = PebbleFont.hero,
                        fontWeight = FontWeight.Bold,
                    )
                    Text(
                        text = store.t("stageNewTabSubtitle"),
                        color = p.text2,
                        fontSize = PebbleFont.body,
                    )
                }

                if (canUseFiles) {
                    StageLauncherCard(
                        icon = PebbleIcons.Folder,
                        title = store.t("stageFilesTab"),
                        description = store.t("stageFilesTabDesc"),
                        inWideStage = inWideStage,
                        onClick = onOpenFiles,
                    )
                }

                StageLauncherCard(
                    icon = PebbleIcons.DesktopWindows,
                    title = store.t("stageDesktopTab"),
                    description = store.t("stageDesktopTabDesc"),
                    inWideStage = inWideStage,
                    onClick = onOpenDesktop,
                )

                if (launcherTasks.isNotEmpty()) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            text = store.t("stageTasksSection"),
                            color = p.mute,
                            fontSize = PebbleFont.caption,
                            fontWeight = FontWeight.SemiBold,
                            modifier = Modifier.weight(1f),
                        )
                        TagPill("${launcherTasks.size}", p.s1, p.text2)
                    }

                    launcherTasks.forEach { task ->
                        val (fg, bg) = taskStatusColors(task.status, p)
                        val dev = taskDeviceLabel(task.deviceId, store.nodes) ?: store.t("taskDeviceLocal")
                        val meta = listOfNotNull(dev, task.harness?.takeIf { it.isNotEmpty() }).joinToString(" · ")
                        val shape = RoundedCornerShape(16.dp)
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(shape)
                                .background(if (inWideStage) p.s1 else p.card)
                                .border(0.5.dp, p.s2, shape)
                                .clickable { onOpenTask(task) }
                                .padding(horizontal = 16.dp, vertical = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                        ) {
                            Box(
                                modifier = Modifier
                                    .size(34.dp)
                                    .clip(RoundedCornerShape(10.dp))
                                    .background(if (inWideStage) p.card else p.s1),
                                contentAlignment = Alignment.Center,
                            ) {
                                Icon(
                                    painter = painterResource(PebbleIcons.List),
                                    contentDescription = null,
                                    tint = p.text,
                                    modifier = Modifier.size(18.dp),
                                )
                            }
                            Column(
                                modifier = Modifier.weight(1f),
                                verticalArrangement = Arrangement.spacedBy(2.dp),
                            ) {
                                Text(
                                    text = task.displayTitle,
                                    color = p.text,
                                    fontSize = PebbleFont.cardTitle,
                                    fontWeight = FontWeight.SemiBold,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                if (meta.isNotEmpty()) {
                                    Text(
                                        text = meta,
                                        color = p.text2,
                                        fontSize = PebbleFont.caption,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                }
                            }
                            TagPill(taskStatusText(task.status, store), bg, fg)
                        }
                    }
                }
            }
        }
    }

    if (inWideStage) {
        Surface(
            color = p.card,
            shape = RoundedCornerShape(20.dp),
            modifier = modifier
                .fillMaxSize()
                .padding(12.dp),
        ) {
            content()
        }
    } else {
        Box(
            modifier = modifier
                .fillMaxSize()
                .background(p.page),
        ) {
            content()
        }
    }
}

@Composable
private fun StageLauncherCard(
    icon: Int,
    title: String,
    description: String,
    inWideStage: Boolean,
    onClick: () -> Unit,
) {
    val p = LocalPalette.current
    val shape = RoundedCornerShape(16.dp)
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(if (inWideStage) p.s1 else p.card)
            .border(0.5.dp, p.s2, shape)
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Box(
            modifier = Modifier
                .size(38.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(if (inWideStage) p.card else p.s1),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                painter = painterResource(icon),
                contentDescription = null,
                tint = p.text,
                modifier = Modifier.size(20.dp),
            )
        }
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(
                text = title,
                color = p.text,
                fontSize = PebbleFont.cardTitle,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                text = description,
                color = p.text2,
                fontSize = PebbleFont.secondary,
            )
        }
        Icon(
            painter = painterResource(PebbleIcons.ChevronRight),
            contentDescription = null,
            tint = p.mute,
            modifier = Modifier.size(18.dp),
        )
    }
}
