import * as Clipboard from 'expo-clipboard';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Image,
  LayoutChangeEvent,
  Linking,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';

import {
  fetchWorkspaceFileContent,
  fetchWorkspaceFiles,
  requestWorkspaceFileTicket,
  workspaceFileRawURL,
} from '../api';
import { useI18n } from '../i18n';
import { MONO_FONT, useTheme } from '../theme';
import type { WorkspaceFileContent, WorkspaceFileEntry, WorkspaceListResult } from '../types';
import { ChevronLeftIcon, DocIcon, FolderIcon } from './Icons';
import { MarkdownText } from './MarkdownText';

interface WorkspaceFilesPaneProps {
  baseURL: string;
  authToken: string;
  botId: string;
  initialPath?: string;
  onQuotePath: (relPath: string) => void;
}

function formatByteSize(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B';
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb.toFixed(1)} KB`;
  return `${(kb / 1024).toFixed(1)} MB`;
}

export function WorkspaceFilesPane({
  baseURL,
  authToken,
  botId,
  initialPath = '',
  onQuotePath,
}: WorkspaceFilesPaneProps) {
  const theme = useTheme();
  const { t } = useI18n();

  const [paneWidth, setPaneWidth] = useState(0);
  const isSplit = paneWidth >= 680;

  const [dirPath, setDirPath] = useState('');
  const [showHidden, setShowHidden] = useState(false);
  const [listData, setListData] = useState<WorkspaceListResult | null>(null);
  const [listLoading, setListLoading] = useState(false);
  const [listErr, setListErr] = useState('');

  const [selectedPath, setSelectedPath] = useState('');
  const [preview, setPreview] = useState<WorkspaceFileContent | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewErr, setPreviewErr] = useState('');
  const [imageUrl, setImageUrl] = useState('');
  const [notice, setNotice] = useState('');

  const listSeqRef = useRef(0);
  const previewSeqRef = useRef(0);

  const loadDir = useCallback(
    async (targetPath: string, hidden: boolean) => {
      if (!baseURL || !botId) return;
      const seq = ++listSeqRef.current;
      setListLoading(true);
      setListErr('');
      try {
        const res = await fetchWorkspaceFiles(baseURL, authToken, botId, targetPath, hidden);
        if (seq !== listSeqRef.current) return;
        setListData(res);
        setDirPath(res.path || '');
      } catch (e: any) {
        if (seq !== listSeqRef.current) return;
        setListErr(e?.message || t('opFailed'));
      } finally {
        if (seq === listSeqRef.current) setListLoading(false);
      }
    },
    [authToken, baseURL, botId, t]
  );

  const openFile = useCallback(
    async (relPath: string) => {
      if (!baseURL || !botId || !relPath) return;
      const seq = ++previewSeqRef.current;
      setSelectedPath(relPath);
      setPreviewLoading(true);
      setPreviewErr('');
      setImageUrl('');
      setNotice('');
      try {
        const doc = await fetchWorkspaceFileContent(baseURL, authToken, botId, relPath);
        if (seq !== previewSeqRef.current) return;
        setPreview(doc);
        if (doc.kind === 'image') {
          const tk = await requestWorkspaceFileTicket(baseURL, authToken, botId, relPath);
          if (seq !== previewSeqRef.current) return;
          setImageUrl(workspaceFileRawURL(baseURL, botId, relPath, tk.ticket, false));
        }
      } catch (e: any) {
        if (seq !== previewSeqRef.current) return;
        setPreview(null);
        setPreviewErr(e?.message || t('opFailed'));
      } finally {
        if (seq === previewSeqRef.current) setPreviewLoading(false);
      }
    },
    [authToken, baseURL, botId, t]
  );

  useEffect(() => {
    setSelectedPath('');
    setPreview(null);
    setPreviewErr('');
    setImageUrl('');
    if (!botId) return;
    if (initialPath) {
      const idx = initialPath.lastIndexOf('/');
      const parent = idx >= 0 ? initialPath.slice(0, idx) : '';
      void loadDir(parent, showHidden);
      void openFile(initialPath);
    } else {
      void loadDir('', showHidden);
    }
  }, [botId, initialPath]);

  const handleDownload = async (relPath: string) => {
    try {
      const tk = await requestWorkspaceFileTicket(baseURL, authToken, botId, relPath);
      const url = workspaceFileRawURL(baseURL, botId, relPath, tk.ticket, true);
      await Linking.openURL(url);
    } catch (e: any) {
      setNotice(e?.message || t('opFailed'));
    }
  };

  const handleCopy = async (relPath: string) => {
    await Clipboard.setStringAsync(relPath);
    setNotice(t('pathCopied'));
  };

  const segments = dirPath ? dirPath.split('/').filter(Boolean) : [];

  const renderListColumn = () => (
    <View style={[styles.colCard, { backgroundColor: theme.card }]}>
      <View style={[styles.colHeader, { borderBottomColor: theme.s2 }]}>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.crumbsRow}>
          <Pressable onPress={() => void loadDir('', showHidden)} style={styles.crumbBtn}>
            <Text style={[styles.crumbTxt, { color: dirPath === '' ? theme.text : theme.text2 }]}>
              {t('rootWorkspace')}
            </Text>
          </Pressable>
          {segments.map((seg, i) => {
            const target = segments.slice(0, i + 1).join('/');
            const last = i === segments.length - 1;
            return (
              <React.Fragment key={target}>
                <Text style={[styles.crumbSep, { color: theme.mute }]}>/</Text>
                <Pressable onPress={() => void loadDir(target, showHidden)} style={styles.crumbBtn}>
                  <Text style={[styles.crumbTxt, { color: last ? theme.text : theme.text2 }]}>{seg}</Text>
                </Pressable>
              </React.Fragment>
            );
          })}
        </ScrollView>
        <View style={styles.headerActs}>
          <Pressable
            onPress={() => {
              const next = !showHidden;
              setShowHidden(next);
              void loadDir(dirPath, next);
            }}
            style={[styles.pillBtn, { backgroundColor: showHidden ? theme.accent : theme.s1 }]}
          >
            <Text style={[styles.pillTxt, { color: showHidden ? theme.accentText : theme.text }]}>
              {t('showHiddenFiles')}
            </Text>
          </Pressable>
          <Pressable
            onPress={() => void loadDir(dirPath, showHidden)}
            style={[styles.pillBtn, { backgroundColor: theme.s1 }]}
          >
            <Text style={[styles.pillTxt, { color: theme.text }]}>{t('refreshFiles')}</Text>
          </Pressable>
        </View>
      </View>

      <ScrollView style={styles.listScroll} contentContainerStyle={styles.listContent}>
        {listLoading && !listData ? (
          <View style={styles.centerState}>
            <ActivityIndicator size="small" color={theme.accent} />
          </View>
        ) : listErr && !listData ? (
          <View style={styles.centerState}>
            <Text style={[styles.stateErr, { color: theme.danger }]}>{listErr}</Text>
            <Pressable
              onPress={() => void loadDir(dirPath, showHidden)}
              style={[styles.retryBtn, { backgroundColor: theme.accent }]}
            >
              <Text style={[styles.retryTxt, { color: theme.accentText }]}>{t('retry')}</Text>
            </Pressable>
          </View>
        ) : !listData?.entries?.length ? (
          <View style={styles.centerState}>
            <FolderIcon size={28} color={theme.mute} />
            <Text style={[styles.emptyTxt, { color: theme.mute }]}>{t('emptyFolder')}</Text>
          </View>
        ) : (
          <>
            {dirPath !== '' && (
              <Pressable
                onPress={() => void loadDir(listData.parent || '', showHidden)}
                style={[styles.row, { borderBottomColor: theme.s2 }]}
              >
                <ChevronLeftIcon size={18} color={theme.text2} />
                <Text style={[styles.rowName, { color: theme.text }]}>..</Text>
              </Pressable>
            )}
            {listData.entries.map((ent: WorkspaceFileEntry) => {
              const selected = isSplit && !ent.isDir && selectedPath === ent.path;
              return (
                <Pressable
                  key={ent.path}
                  onPress={() => {
                    if (ent.isDir) void loadDir(ent.path, showHidden);
                    else void openFile(ent.path);
                  }}
                  style={[
                    styles.row,
                    {
                      borderBottomColor: theme.s2,
                      backgroundColor: selected ? theme.sel : 'transparent',
                    },
                  ]}
                >
                  {ent.isDir ? (
                    <FolderIcon size={20} color={ent.isShared ? theme.lilacInk : theme.text2} />
                  ) : (
                    <DocIcon size={20} color={theme.text2} />
                  )}
                  <View style={styles.rowMain}>
                    <Text style={[styles.rowName, { color: theme.text }]} numberOfLines={1}>
                      {ent.name}
                    </Text>
                    {(ent.isShared || !ent.isDir) && (
                      <View style={styles.rowMeta}>
                        {ent.isShared ? (
                          <View style={[styles.badge, { backgroundColor: theme.lilac }]}>
                            <Text style={[styles.badgeTxt, { color: theme.lilacInk }]}>{t('sharedBadge')}</Text>
                          </View>
                        ) : null}
                        {!ent.isDir ? (
                          <Text style={[styles.metaTxt, { color: theme.mute }]}>{formatByteSize(ent.size)}</Text>
                        ) : null}
                      </View>
                    )}
                  </View>
                </Pressable>
              );
            })}
          </>
        )}
      </ScrollView>
    </View>
  );

  const renderPreviewColumn = () => {
    if (!selectedPath) {
      return (
        <View style={[styles.colCard, styles.centerState, { backgroundColor: theme.card }]}>
          <DocIcon size={30} color={theme.mute} />
          <Text style={[styles.emptyTxt, { color: theme.mute }]}>{t('selectFileHint')}</Text>
        </View>
      );
    }

    const baseName = selectedPath.split('/').pop() || selectedPath;
    return (
      <View style={[styles.colCard, { backgroundColor: theme.card }]}>
        <View style={[styles.previewHd, { borderBottomColor: theme.s2 }]}>
          {!isSplit && (
            <Pressable
              onPress={() => {
                setSelectedPath('');
                setPreview(null);
                setImageUrl('');
              }}
              style={styles.backBtn}
            >
              <ChevronLeftIcon size={20} color={theme.text} />
            </Pressable>
          )}
          <View style={styles.previewTitles}>
            <Text style={[styles.previewName, { color: theme.text }]} numberOfLines={1}>
              {baseName}
            </Text>
            {selectedPath.includes('/') && (
              <Text style={[styles.previewSub, { color: theme.mute }]} numberOfLines={1}>
                {selectedPath}
              </Text>
            )}
          </View>
          <View style={styles.headerActs}>
            <Pressable
              onPress={() => onQuotePath(selectedPath)}
              style={[styles.pillBtn, { backgroundColor: theme.s1 }]}
            >
              <Text style={[styles.pillTxt, { color: theme.text }]}>{t('insertToChat')}</Text>
            </Pressable>
            <Pressable
              onPress={() => void handleCopy(selectedPath)}
              style={[styles.pillBtn, { backgroundColor: theme.s1 }]}
            >
              <Text style={[styles.pillTxt, { color: theme.text }]}>{t('copyPath')}</Text>
            </Pressable>
            <Pressable
              onPress={() => void handleDownload(selectedPath)}
              style={[styles.pillBtn, { backgroundColor: theme.accent }]}
            >
              <Text style={[styles.pillTxt, { color: theme.accentText }]}>{t('downloadFile')}</Text>
            </Pressable>
          </View>
        </View>

        {notice ? <Text style={[styles.noticeTxt, { color: theme.mute }]}>{notice}</Text> : null}

        <ScrollView style={styles.previewBody} contentContainerStyle={styles.previewContent}>
          {previewLoading ? (
            <View style={styles.centerState}>
              <ActivityIndicator size="small" color={theme.accent} />
            </View>
          ) : previewErr ? (
            <View style={styles.centerState}>
              <Text style={[styles.stateErr, { color: theme.danger }]}>{previewErr}</Text>
              <Pressable
                onPress={() => void openFile(selectedPath)}
                style={[styles.retryBtn, { backgroundColor: theme.accent }]}
              >
                <Text style={[styles.retryTxt, { color: theme.accentText }]}>{t('retry')}</Text>
              </Pressable>
            </View>
          ) : !preview ? null : preview.kind === 'image' ? (
            imageUrl ? (
              <Image source={{ uri: imageUrl }} style={styles.previewImage} resizeMode="contain" />
            ) : null
          ) : preview.kind === 'binary' ? (
            <View style={[styles.binaryCard, { backgroundColor: theme.s1 }]}>
              <Text style={[styles.binaryName, { color: theme.text }]}>{preview.name}</Text>
              <Text style={[styles.binaryMeta, { color: theme.mute }]}>
                {`${preview.mimeType} · ${formatByteSize(preview.size)}`}
              </Text>
              <Text style={[styles.binaryHint, { color: theme.text2 }]}>{t('binaryFileHint')}</Text>
              <Pressable
                onPress={() => void handleDownload(preview.path)}
                style={[styles.dlBtn, { backgroundColor: theme.accent }]}
              >
                <Text style={[styles.dlBtnTxt, { color: theme.accentText }]}>{t('downloadFile')}</Text>
              </Pressable>
            </View>
          ) : (
            <View>
              {preview.truncated ? (
                <View style={[styles.truncNote, { backgroundColor: theme.s1 }]}>
                  <Text style={[styles.truncTxt, { color: theme.text2 }]}>
                    {t('fileTruncatedHint', { size: formatByteSize(preview.content?.length || 0) })}
                  </Text>
                </View>
              ) : null}
              {preview.kind === 'markdown' ? (
                <MarkdownText text={preview.content || ''} theme={theme} />
              ) : (
                <Text selectable style={[styles.codeTxt, { color: theme.text }]}>
                  {preview.content || ''}
                </Text>
              )}
            </View>
          )}
        </ScrollView>
      </View>
    );
  };

  return (
    <View
      onLayout={(e: LayoutChangeEvent) => setPaneWidth(e.nativeEvent.layout.width)}
      style={[styles.root, { backgroundColor: theme.page }]}
    >
      {isSplit ? (
        <View style={styles.splitRow}>
          <View style={styles.splitLeft}>{renderListColumn()}</View>
          <View style={styles.splitRight}>{renderPreviewColumn()}</View>
        </View>
      ) : (
        <View style={styles.singleWrap}>
          {selectedPath ? renderPreviewColumn() : renderListColumn()}
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  splitRow: { flex: 1, flexDirection: 'row', gap: 12, padding: 12 },
  splitLeft: { width: 288, minHeight: 0 },
  splitRight: { flex: 1, minWidth: 0, minHeight: 0 },
  singleWrap: { flex: 1, padding: 12 },
  colCard: { flex: 1, borderRadius: 20, overflow: 'hidden' },
  colHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
    paddingHorizontal: 12,
    paddingVertical: 10,
    borderBottomWidth: 1,
  },
  headerActs: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  crumbsRow: { alignItems: 'center', gap: 4, paddingRight: 8 },
  crumbBtn: { paddingHorizontal: 4, paddingVertical: 4 },
  crumbTxt: { fontSize: 14, fontWeight: '600' },
  crumbSep: { fontSize: 13 },
  pillBtn: { borderRadius: 999, paddingHorizontal: 10, paddingVertical: 6 },
  pillTxt: { fontSize: 13, fontWeight: '600' },
  listScroll: { flex: 1 },
  listContent: { flexGrow: 1 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingHorizontal: 14,
    paddingVertical: 12,
    borderBottomWidth: 1,
  },
  rowMain: { flex: 1, minWidth: 0, gap: 2 },
  rowName: { fontSize: 16, fontWeight: '600' },
  rowMeta: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  badge: { borderRadius: 999, paddingHorizontal: 6, paddingVertical: 1 },
  badgeTxt: { fontSize: 12, fontWeight: '600' },
  metaTxt: { fontSize: 13 },
  centerState: {
    flex: 1,
    paddingVertical: 40,
    paddingHorizontal: 20,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 10,
  },
  emptyTxt: { textAlign: 'center', fontSize: 15 },
  stateErr: { textAlign: 'center', fontSize: 15 },
  retryBtn: { borderRadius: 999, paddingHorizontal: 16, paddingVertical: 8 },
  retryTxt: { fontSize: 14, fontWeight: '600' },
  previewHd: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingHorizontal: 14,
    paddingVertical: 10,
    borderBottomWidth: 1,
    flexWrap: 'wrap',
  },
  backBtn: { padding: 4 },
  previewTitles: { flex: 1, minWidth: 120, gap: 1 },
  previewName: { fontSize: 16, fontWeight: '600' },
  previewSub: { fontSize: 12, fontFamily: MONO_FONT },
  noticeTxt: { textAlign: 'center', fontSize: 13, paddingTop: 6 },
  previewBody: { flex: 1 },
  previewContent: { padding: 16, flexGrow: 1 },
  truncNote: { borderRadius: 10, padding: 10, marginBottom: 10 },
  truncTxt: { fontSize: 13 },
  codeTxt: { fontSize: 14, lineHeight: 21, fontFamily: MONO_FONT },
  previewImage: { width: '100%', height: 380, borderRadius: 12 },
  binaryCard: { borderRadius: 16, padding: 20, alignItems: 'center', gap: 10 },
  binaryName: { fontSize: 17, fontWeight: '700' },
  binaryMeta: { fontSize: 13, fontFamily: MONO_FONT },
  binaryHint: { fontSize: 14, textAlign: 'center' },
  dlBtn: { borderRadius: 999, paddingHorizontal: 16, paddingVertical: 10, marginTop: 4 },
  dlBtnTxt: { fontSize: 15, fontWeight: '600' },
});
