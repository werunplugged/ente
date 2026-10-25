import ArrowBackIcon from "@mui/icons-material/ArrowBack";
import { IconButton, Stack, Tooltip, Typography } from "@mui/material";
import type { CollectionSummary } from "ente-new/photos/services/collection-summary";
import { t } from "i18next";
import { useEffect, useRef, useState } from "react";
import { AlbumTile } from "./AlbumTile";

interface HiddenAlbumsHeaderProps {
    albums: CollectionSummary[];
    onSelectAlbum: (id: number) => void;
    onBack: () => void;
}

// Includes the spacing before the hidden-items summary in the virtualized list.
export const hiddenAlbumsHeaderHeight = 311;

export const HiddenAlbumsHeader = ({
    albums,
    onSelectAlbum,
    onBack,
}: HiddenAlbumsHeaderProps) => {
    const rowRef = useRef<HTMLDivElement>(null);
    const [hasMoreAlbums, setHasMoreAlbums] = useState(false);

    useEffect(() => {
        const row = rowRef.current;
        if (!row) return;

        const updateFade = () =>
            setHasMoreAlbums(
                row.scrollWidth - row.clientWidth - row.scrollLeft > 1,
            );
        updateFade();
        row.addEventListener("scroll", updateFade, { passive: true });
        const observer = new ResizeObserver(updateFade);
        observer.observe(row);
        return () => {
            row.removeEventListener("scroll", updateFade);
            observer.disconnect();
        };
    }, [albums.length]);

    return (
        <Stack
            sx={{
                height: albums.length ? hiddenAlbumsHeaderHeight : 78,
                gap: "24px",
            }}
        >
            <Stack
                direction="row"
                sx={{ alignItems: "center", minHeight: 54, gap: 1 }}
            >
                <Tooltip title={t("go_back")}>
                    <IconButton
                        aria-label={t("go_back")}
                        onClick={onBack}
                        sx={{ width: 32, height: 32 }}
                    >
                        <ArrowBackIcon sx={{ fontSize: 20 }} />
                    </IconButton>
                </Tooltip>
                <Typography
                    component="h2"
                    sx={{
                        fontFamily: '"Outfit Variable", Outfit, sans-serif',
                        fontWeight: 600,
                        fontSize: 24,
                        lineHeight: "32px",
                    }}
                >
                    {t("hidden_albums")}
                </Typography>
            </Stack>
            <Stack
                ref={rowRef}
                direction="row"
                sx={{
                    "--album-name-size": "14px",
                    "--album-count-size": "12px",
                    "--album-text-gap": "4px",
                    gap: "22px",
                    overflowX: "auto",
                    pb: "12px",
                    minHeight: 0,
                    maskImage: hasMoreAlbums
                        ? "linear-gradient(to right, black calc(100% - 110px), transparent 100%)"
                        : undefined,
                }}
            >
                {albums.map((album) => (
                    <AlbumTile
                        key={album.id}
                        album={album}
                        onSelectAlbum={onSelectAlbum}
                        showBadges={false}
                        countLabel={t("photos_count", {
                            count: album.fileCount,
                        })}
                    />
                ))}
            </Stack>
        </Stack>
    );
};
