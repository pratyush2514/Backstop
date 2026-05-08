"use client";

import Lottie from "lottie-react";
import refreshArrowsData from "../public/animations/refresh-arrows.json";

export default function LottieRefresh({ className }: { className?: string }) {
  return (
    <div className={className} aria-hidden="true">
      <Lottie animationData={refreshArrowsData} loop autoplay style={{ width: "100%", height: "100%" }} />
    </div>
  );
}
