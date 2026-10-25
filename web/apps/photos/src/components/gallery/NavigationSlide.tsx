import { Box } from "@mui/material";
import type { PropsWithChildren } from "react";
import {
    navigationMotion,
    useNavigationContentMotion,
} from "./use-navigation-content-motion";

export const NavigationSlide: React.FC<
    PropsWithChildren<{
        open: boolean;
        width: number | { xs: number; sm: number };
        reduceMotion: boolean;
        rail?: boolean;
    }>
> = ({ open, width, reduceMotion, rail, children }) => {
    const positionRef = useNavigationContentMotion(open, reduceMotion);
    return (
        <Box
            ref={positionRef}
            inert={!open}
            sx={{
                position: "relative",
                flexShrink: 0,
                width: open ? width : 0,
                height: "100%",
                pointerEvents: open ? undefined : "none",
                zIndex: rail ? 2 : 1,
            }}
        >
            <Box
                sx={{
                    position: "absolute",
                    inset: "0 auto 0 0",
                    width,
                    overflow: "clip",
                }}
            >
                <Box
                    sx={{
                        height: "100%",
                        transform:
                            open || reduceMotion
                                ? "translateX(0)"
                                : "translateX(-100%)",
                        opacity: open ? 1 : 0,
                        transition: reduceMotion
                            ? `opacity ${navigationMotion.fadeDuration}ms ${navigationMotion.easing}`
                            : `transform ${navigationMotion.duration}ms ${navigationMotion.easing}, opacity ${navigationMotion.fadeDuration}ms ${navigationMotion.easing}`,
                    }}
                >
                    {children}
                </Box>
            </Box>
        </Box>
    );
};
