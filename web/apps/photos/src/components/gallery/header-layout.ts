import type { CollectionSummary } from "ente-new/photos/services/collection-summary";

export const isAlbumSummary = ({ type }: CollectionSummary) =>
    type == "album" || type == "folder" || type == "sharedIncoming";

export const twoLineMobileTitleSx = {
    overflowWrap: "anywhere",
    minHeight: { xs: "64px", sm: 0 },
    display: { xs: "-webkit-box", sm: "block" },
    overflow: { xs: "hidden", sm: "visible" },
    WebkitBoxOrient: "vertical",
    WebkitLineClamp: { xs: 2, sm: "unset" },
} as const;
